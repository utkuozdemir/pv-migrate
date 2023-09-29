//go:build integration

//nolint:paralleltest
package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/remotecommand"
	watchtools "k8s.io/client-go/tools/watch"
	"k8s.io/utils/env"

	"github.com/utkuozdemir/pv-migrate/app"
	"github.com/utkuozdemir/pv-migrate/k8s"
	applog "github.com/utkuozdemir/pv-migrate/log"
	"github.com/utkuozdemir/pv-migrate/util"
)

const (
	dataFileUID         = "12345"
	dataFileGID         = "54321"
	dataFilePath        = "/volume/file.txt"
	extraDataFilePath   = "/volume/extra_file.txt"
	generateDataContent = "DATA"

	longSourcePvcName = "source-source-source-source-source-source-source-source-source-source-source-source-" +
		"source-source-source-source-source-source-source-source-source-source-source-source-source-source-source"
	longDestPvcName = "dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-" +
		"dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest-dest"

	migrateCmdline            = "--log-level debug --log-format json migrate "
	migrateCmdlineWithNetpols = migrateCmdline +
		"--helm-set rsync.networkPolicy.enabled=true " +
		"--helm-set sshd.networkPolicy.enabled=true"
        migrateCmdlineWithNetpolsAndRsyncFixPrivateKeyPerms = migrateCmdlineWithNetpols +
                " --helm-set rsync.fixPrivateKeyPerms=true"
)

var (
	ns1 string
	ns2 string
	ns3 string

	extraClusterKubeconfig string

	mainClusterCli  *k8s.ClusterClient
	extraClusterCli *k8s.ClusterClient

	generateDataShellCommand = fmt.Sprintf("echo -n %s > %s && chown %s:%s %s",
		generateDataContent, dataFilePath, dataFileUID, dataFileGID, dataFilePath)
	generateExtraDataShellCommand = fmt.Sprintf("echo -n %s > %s",
		generateDataContent, extraDataFilePath)
	printDataUIDGIDContentShellCommand = fmt.Sprintf("stat -c '%%u' %s && stat -c '%%g' %s && cat %s",
		dataFilePath, dataFilePath, dataFilePath)
	checkExtraDataShellCommand = "ls " + extraDataFilePath
	clearDataShellCommand      = "find /volume -mindepth 1 -delete"

	resourceLabels = map[string]string{
		"pv-migrate-test": "true",
	}

	ErrPodExecStderr          = errors.New("pod exec stderr")
	ErrUnexpectedTypePVCWatch = errors.New("unexpected type while watching PVC")
	ErrUnexpectedTypePodWatch = errors.New("unexpected type while watching pod")
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := setup(ctx)
	if err != nil {
		if teardownErr := teardown(ctx); teardownErr != nil {
			log.Errorf("failed to tearddown after test context init failure: %v", teardownErr)
		}

		log.Fatalf("failed to initialize test context: %v", err)
	}

	code := m.Run()

	err = teardown(ctx)
	if err != nil {
		log.Errorf("failed to teardown after tests: %v", err)
	}

	os.Exit(code)
}

func TestSameNS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("%s -i -n %s -N %s source dest", migrateCmdlineWithNetpols, ns1, ns1)
	assert.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "dest", printDataUIDGIDContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

// TestCustomRsyncArgs is the same as TestSameNS except it also passes custom args to rsync.
func TestCustomRsyncArgs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmdArgs := strings.Fields(fmt.Sprintf("%s -i -n %s -N %s", migrateCmdlineWithNetpols, ns1, ns1))
	cmdArgs = append(cmdArgs, "--helm-set", "rsync.extraArgs=--partial --inplace --sparse", "source", "dest")

	assert.NoError(t, runCliAppWithArgs(ctx, cmdArgs...))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "dest", printDataUIDGIDContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestSameNSLbSvc(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("%s -s lbsvc -i -n %s -N %s --lbsvc-timeout 5m source dest", migrateCmdlineWithNetpolsAndRsyncFixPrivateKeyPerms, ns1, ns1)
	assert.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "dest", printDataUIDGIDContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestNoChown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("%s -i -o -n %s -N %s source dest", migrateCmdlineWithNetpols, ns1, ns1)
	assert.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "dest", printDataUIDGIDContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, "0", parts[0])
	assert.Equal(t, "0", parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestDeleteExtraneousFiles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("%s -d -i -n %s -N %s source dest", migrateCmdlineWithNetpols, ns1, ns1)
	assert.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "dest", printDataUIDGIDContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "No such file or directory")
}

func TestMountedError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("%s -n %s -N %s source dest", migrateCmdlineWithNetpols, ns1, ns1)
	err = runCliApp(ctx, cmd)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ignore-mounted is not requested")
}

func TestDifferentNS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	_, err := execInPod(ctx, mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("%s -i -n %s -N %s source dest", migrateCmdlineWithNetpols, ns1, ns2)
	assert.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns2, "dest", printDataUIDGIDContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestFailWithoutNetworkPolicies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	_, err := execInPod(ctx, mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("%s -i -n %s -N %s source dest", migrateCmdline, ns1, ns2)
	assert.Error(t, runCliApp(ctx, cmd))
}

func TestLbSvcDestHostOverride(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	svcName := "alternative-svc"
	_, err := mainClusterCli.KubeClient.CoreV1().Services(ns1).Create(context.Background(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:   svcName,
				Labels: resourceLabels,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app.kubernetes.io/component": "sshd",
					"app.kubernetes.io/name":      "pv-migrate",
				},
				Ports: []corev1.ServicePort{
					{
						Name:       "ssh",
						Port:       22,
						TargetPort: intstr.FromInt(22),
					},
				},
			},
		}, metav1.CreateOptions{})
	assert.NoError(t, err)

	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	destHostOverride := svcName + "." + ns1
	cmd := fmt.Sprintf(
		"%s -i -n %s -N %s -H %s source dest", migrateCmdlineWithNetpols, ns1, ns2, destHostOverride)
	assert.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns2, "dest", printDataUIDGIDContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestRSA(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	_, err := execInPod(ctx, mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("%s -a rsa -i -n %s -N %s source dest", migrateCmdlineWithNetpols, ns1, ns2)
	assert.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns2, "dest", printDataUIDGIDContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestDifferentCluster(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	_, err := execInPod(ctx, extraClusterCli, ns3, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("%s -K %s -i -n %s -N %s source dest", migrateCmdlineWithNetpols,
		extraClusterKubeconfig, ns1, ns3)
	assert.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, extraClusterCli, ns3, "dest", printDataUIDGIDContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, extraClusterCli, ns3, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestLocal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	assert.NoError(t, clearDests(ctx))

	_, err := execInPod(ctx, extraClusterCli, ns3, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("%s -K %s -s local -i -n %s -N %s source dest", migrateCmdlineWithNetpols,
		extraClusterKubeconfig, ns1, ns3)
	assert.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, extraClusterCli, ns3, "dest", printDataUIDGIDContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ctx, extraClusterCli, ns3, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestLongPVCNames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	_, err := execInPod(ctx, mainClusterCli, ns1, "long-dest", clearDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("%s -i -n %s -N %s %s %s",
		migrateCmdlineWithNetpols, ns1, ns1, longSourcePvcName, longDestPvcName)
	assert.NoError(t, runCliApp(ctx, cmd))

	stdout, err := execInPod(ctx, mainClusterCli, ns1, "long-dest", printDataUIDGIDContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)

	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUID, parts[0])
	assert.Equal(t, dataFileGID, parts[1])
	assert.Equal(t, generateDataContent, parts[2])
}

func setup(ctx context.Context) error {
	homeDir, err := userHomeDir()
	if err != nil {
		return err
	}

	extraClusterKubeconfig = env.GetString("PVMIG_TEST_EXTRA_KUBECONFIG", homeDir+"/.kube/config")

	mainCli, err := k8s.GetClusterClient("", "")
	if err != nil {
		return fmt.Errorf("failed to get main cluster client: %w", err)
	}

	mainClusterCli = mainCli

	extraCli, err := k8s.GetClusterClient(extraClusterKubeconfig, "")
	if err != nil {
		return fmt.Errorf("failed to get extra cluster client: %w", err)
	}

	extraClusterCli = extraCli

	if mainCli.RestConfig.Host == extraCli.RestConfig.Host {
		log.Warnf("WARNING: USING A SINGLE CLUSTER FOR INTEGRATION TESTS!")
	}

	ns1 = "pv-migrate-test-1-" + util.RandomHexadecimalString(5)
	ns2 = "pv-migrate-test-2-" + util.RandomHexadecimalString(5)
	ns3 = "pv-migrate-test-3-" + util.RandomHexadecimalString(5)

	err = setupNSs(ctx)
	if err != nil {
		return err
	}

	err = setupPVCs(ctx)
	if err != nil {
		return err
	}

	err = setupPods(ctx)
	if err != nil {
		return err
	}

	err = setupWaitForPVCs()
	if err != nil {
		return err
	}

	err = setupWaitForPods()
	if err != nil {
		return err
	}

	_, err = execInPod(ctx, mainClusterCli, ns1, "source", generateDataShellCommand)

	return err
}

func setupNSs(ctx context.Context) error {
	if err := createNS(ctx, mainClusterCli, ns1); err != nil {
		return err
	}

	if err := createNS(ctx, mainClusterCli, ns2); err != nil {
		return err
	}

	return createNS(ctx, extraClusterCli, ns3)
}

func setupWaitForPVCs() error {
	err := waitUntilPVCIsBound(mainClusterCli, ns1, "source")
	if err != nil {
		return err
	}

	err = waitUntilPVCIsBound(mainClusterCli, ns1, "dest")
	if err != nil {
		return err
	}

	err = waitUntilPVCIsBound(mainClusterCli, ns2, "dest")
	if err != nil {
		return err
	}

	return waitUntilPVCIsBound(extraClusterCli, ns3, "dest")
}

func setupWaitForPods() error {
	err := waitUntilPodIsRunning(mainClusterCli, ns1, "source")
	if err != nil {
		return err
	}

	err = waitUntilPodIsRunning(mainClusterCli, ns1, "dest")
	if err != nil {
		return err
	}

	err = waitUntilPodIsRunning(mainClusterCli, ns2, "dest")
	if err != nil {
		return err
	}

	return waitUntilPodIsRunning(extraClusterCli, ns3, "dest")
}

func setupPods(ctx context.Context) error {
	if err := createPod(ctx, mainClusterCli, ns1, "source", "source"); err != nil {
		return err
	}

	if err := createPod(ctx, mainClusterCli, ns1, "dest", "dest"); err != nil {
		return err
	}

	if err := createPod(ctx, mainClusterCli, ns2, "dest", "dest"); err != nil {
		return err
	}

	return createPod(ctx, extraClusterCli, ns3, "dest", "dest")
}

func setupPVCs(ctx context.Context) error {
	err := setupPVCsWithLongName(ctx)
	if err != nil {
		return err
	}

	if err = createPVC(ctx, mainClusterCli, ns1, "source"); err != nil {
		return err
	}

	if err = createPVC(ctx, mainClusterCli, ns1, "dest"); err != nil {
		return err
	}

	if err = createPVC(ctx, mainClusterCli, ns2, "dest"); err != nil {
		return err
	}

	return createPVC(ctx, extraClusterCli, ns3, "dest")
}

func setupPVCsWithLongName(ctx context.Context) error {
	if err := createPVC(ctx, mainClusterCli, ns1, longSourcePvcName); err != nil {
		return err
	}

	if err := createPVC(ctx, mainClusterCli, ns1, longDestPvcName); err != nil {
		return err
	}

	if err := createPod(ctx, mainClusterCli, ns1, "long-source", longSourcePvcName); err != nil {
		return err
	}

	if err := createPod(ctx, mainClusterCli, ns1, "long-dest", longDestPvcName); err != nil {
		return err
	}

	if err := waitUntilPodIsRunning(mainClusterCli, ns1, "long-source"); err != nil {
		return err
	}

	if err := waitUntilPodIsRunning(mainClusterCli, ns1, "long-dest"); err != nil {
		return err
	}

	_, err := execInPod(ctx, mainClusterCli, ns1, "long-source", generateDataShellCommand)

	return err
}

func teardown(ctx context.Context) error {
	var result *multierror.Error

	err := deleteNS(ctx, mainClusterCli, ns1)
	if err != nil {
		result = multierror.Append(result, err)
	}

	err = deleteNS(ctx, mainClusterCli, ns2)
	if err != nil {
		result = multierror.Append(result, err)
	}

	err = deleteNS(ctx, extraClusterCli, ns3)
	if err != nil {
		result = multierror.Append(result, err)
	}

	if err = result.ErrorOrNil(); err != nil {
		return fmt.Errorf("failed to teardown: %w", err)
	}

	return nil
}

func userHomeDir() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}

	return usr.HomeDir, nil
}

func createPod(ctx context.Context, cli *k8s.ClusterClient, namespace string, name string, pvc string) error {
	terminationGracePeriodSeconds := int64(0)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    resourceLabels,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
			Volumes: []corev1.Volume{
				{
					Name: "volume",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "main",
					Image:   "docker.io/busybox:stable",
					Command: []string{"tail", "-f", "/dev/null"},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "volume",
							MountPath: "/volume",
						},
					},
				},
			},
		},
	}

	if _, err := cli.KubeClient.CoreV1().
		Pods(namespace).Create(ctx, &pod, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create pod %s: %w", name, err)
	}

	return nil
}

func createPVC(ctx context.Context, cli *k8s.ClusterClient, namespace string, name string) error {
	var storageClassRef *string

	storageClass := os.Getenv("PVMIG_TEST_STORAGE_CLASS")
	if storageClass != "" {
		storageClassRef = &storageClass
	}

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    resourceLabels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: storageClassRef,
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					"storage": resource.MustParse("64Mi"),
				},
			},
		},
	}

	if _, err := cli.KubeClient.CoreV1().PersistentVolumeClaims(namespace).
		Create(ctx, &pvc, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create PVC: %w", err)
	}

	return nil
}

//nolint:dupl
func waitUntilPodIsRunning(cli *k8s.ClusterClient, namespace string, name string) error {
	resCli := cli.KubeClient.CoreV1().Pods(namespace)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector

			list, err := resCli.List(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to list pods: %w", err)
			}

			return list, nil
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector

			cliWatch, err := resCli.Watch(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to watch pods: %w", err)
			}

			return cliWatch, nil
		},
	}

	if _, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.Pod{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Pod)
			if !ok {
				return false, fmt.Errorf("%w: %s/%s", ErrUnexpectedTypePodWatch, namespace, name)
			}

			return res.Status.Phase == corev1.PodRunning, nil
		}); err != nil {
		return fmt.Errorf("failed to wait until pod is running: %w", err)
	}

	return nil
}

//nolint:dupl
func waitUntilPVCIsBound(cli *k8s.ClusterClient, namespace string, name string) error {
	resCli := cli.KubeClient.CoreV1().PersistentVolumeClaims(namespace)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	listWatch := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector

			list, err := resCli.List(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to list PVC: %w", err)
			}

			return list, nil
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector

			cliWatch, err := resCli.Watch(ctx, options)
			if err != nil {
				return nil, fmt.Errorf("failed to watch PVC: %w", err)
			}

			return cliWatch, nil
		},
	}

	if _, err := watchtools.UntilWithSync(ctx, listWatch, &corev1.PersistentVolumeClaim{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.PersistentVolumeClaim)
			if !ok {
				return false, fmt.Errorf("%w: %s/%s", ErrUnexpectedTypePVCWatch, namespace, name)
			}

			return res.Status.Phase == corev1.ClaimBound, nil
		}); err != nil {
		return fmt.Errorf("failed to wait until PVC is bound: %w", err)
	}

	return nil
}

func execInPod(ctx context.Context, cli *k8s.ClusterClient, ns string, name string, cmd string) (string, error) {
	stdoutBuffer := new(bytes.Buffer)
	stderrBuffer := new(bytes.Buffer)

	req := cli.KubeClient.CoreV1().RESTClient().Post().Resource("pods").
		Name(name).Namespace(ns).SubResource("exec")
	option := &corev1.PodExecOptions{
		Command: []string{"sh", "-c", cmd},
		Stdin:   false,
		Stdout:  true,
		Stderr:  true,
		TTY:     false,
	}

	req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)

	config, err := cli.RESTClientGetter.ToRESTConfig()
	if err != nil {
		return "", fmt.Errorf("failed to get REST config: %w", err)
	}

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var result *multierror.Error

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: stdoutBuffer, Stderr: stderrBuffer})
	if err != nil {
		result = multierror.Append(result, err)
	}

	stdout := stdoutBuffer.String()
	stderr := stderrBuffer.String()

	if stderr != "" {
		result = multierror.Append(result, fmt.Errorf("%w: %s", ErrPodExecStderr, stderr))
	}

	if err = result.ErrorOrNil(); err != nil {
		return "", fmt.Errorf("failed to execute command: %w", err)
	}

	return stdout, nil
}

func clearDests(ctx context.Context) error {
	_, err := execInPod(ctx, mainClusterCli, ns1, "dest", clearDataShellCommand)
	if err != nil {
		return err
	}

	_, err = execInPod(ctx, mainClusterCli, ns2, "dest", clearDataShellCommand)
	if err != nil {
		return err
	}

	_, err = execInPod(ctx, extraClusterCli, ns3, "dest", clearDataShellCommand)

	return err
}

func createNS(ctx context.Context, cli *k8s.ClusterClient, name string) error {
	if _, err := cli.KubeClient.CoreV1().
		Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: resourceLabels,
		},
	}, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create namespace %s: %w", name, err)
	}

	return nil
}

func deleteNS(ctx context.Context, cli *k8s.ClusterClient, name string) error {
	namespaces := cli.KubeClient.CoreV1().Namespaces()

	if err := namespaces.Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete namespace %s: %w", name, err)
	}

	return nil
}

func runCliApp(ctx context.Context, cmd string) error {
	return runCliAppWithArgs(ctx, strings.Fields(cmd)...)
}

func runCliAppWithArgs(ctx context.Context, args ...string) error {
	logger, err := applog.New(ctx)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	cliApp := app.New(logger, "", "", "")
	cliApp.SetArgs(args)

	if err = cliApp.Execute(); err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}

	return nil
}
