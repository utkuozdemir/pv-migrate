package integrationtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/app"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/remotecommand"
	watchtools "k8s.io/client-go/tools/watch"
	"k8s.io/utils/env"
	"os"
	"os/user"
	"strings"
	"testing"
	"time"
)

const (
	dataFileUid         = "12345"
	dataFileGid         = "54321"
	dataFilePath        = "/volume/file.txt"
	extraDataFilePath   = "/volume/extra_file.txt"
	generateDataContent = "DATA"
)

var (
	ns1 string
	ns2 string
	ns3 string

	extraClusterKubeconfig string

	mainClusterCli  *k8s.ClusterClient
	extraClusterCli *k8s.ClusterClient

	generateDataShellCommand = fmt.Sprintf("echo -n %s > %s && chown %s:%s %s",
		generateDataContent, dataFilePath, dataFileUid, dataFileGid, dataFilePath)
	generateExtraDataShellCommand = fmt.Sprintf("echo -n %s > %s",
		generateDataContent, extraDataFilePath)
	printDataUidGidContentShellCommand = fmt.Sprintf("stat -c '%%u' %s && stat -c '%%g' %s && cat %s",
		dataFilePath, dataFilePath, dataFilePath)
	checkExtraDataShellCommand = "ls " + extraDataFilePath
	clearDataShellCommand      = "find /volume -mindepth 1 -delete"
)

func TestMain(m *testing.M) {
	err := setup()
	if err != nil {
		log.Fatalf("failed to initialize test context: %v", err)
	}
	code := m.Run()
	err = teardown()
	if err != nil {
		log.Errorf("failed to teardown after tests: %v", err)
	}

	os.Exit(code)
}

func TestSameNS(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug m -i -n %s -N %s source dest", ns1, ns1)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(mainClusterCli, ns1, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestSameNSLbSvc(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug m -s lbsvc -i -n %s -N %s source dest", ns1, ns1)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(mainClusterCli, ns1, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestNoChown(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -i -o -n %s -N %s source dest", ns1, ns1)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(mainClusterCli, ns1, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, "0", parts[0])
	assert.Equal(t, "0", parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestDeleteExtraneousFiles(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -d -i -n %s -N %s source dest", ns1, ns1)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(mainClusterCli, ns1, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(mainClusterCli, ns1, "dest", checkExtraDataShellCommand)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "No such file or directory")
}

func TestMountedError(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(mainClusterCli, ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -n %s -N %s source dest", ns1, ns1)
	err = runCliApp(cmd)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ignore-mounted is not requested")
}

func TestDifferentNS(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -i -n %s -N %s source dest", ns1, ns2)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(mainClusterCli, ns2, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(mainClusterCli, ns2, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestRSA(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(mainClusterCli, ns2, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -a rsa -i -n %s -N %s source dest", ns1, ns2)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(mainClusterCli, ns2, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(mainClusterCli, ns2, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestDifferentCluster(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(extraClusterCli, ns3, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -K %s -i -n %s -N %s source dest",
		extraClusterKubeconfig, ns1, ns3)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(extraClusterCli, ns3, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(extraClusterCli, ns3, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestLocal(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(extraClusterCli, ns3, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -K %s -s local -i -n %s -N %s source dest",
		extraClusterKubeconfig, ns1, ns3)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(extraClusterCli, ns3, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(extraClusterCli, ns3, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func setup() error {
	homeDir, err := userHomeDir()
	if err != nil {
		return err
	}

	extraClusterKubeconfig = env.GetString("PVMIG_TEST_EXTRA_KUBECONFIG", homeDir+"/.kube/config")

	mainCli, err := k8s.GetClusterClient("", "")
	if err != nil {
		return err
	}
	mainClusterCli = mainCli

	extraCli, err := k8s.GetClusterClient(extraClusterKubeconfig, "")
	if err != nil {
		return err
	}
	extraClusterCli = extraCli

	if mainCli.RestConfig.Host == extraCli.RestConfig.Host {
		log.Warnf("WARNING: USING A SINGLE CLUSTER FOR INTEGRATION TESTS!")
	}

	ns1 = "pv-migrate-test-1-" + util.RandomHexadecimalString(5)
	ns2 = "pv-migrate-test-2-" + util.RandomHexadecimalString(5)
	ns3 = "pv-migrate-test-3-" + util.RandomHexadecimalString(5)

	_, err = createNs(mainClusterCli, ns1)
	if err != nil {
		return err
	}

	_, err = createNs(mainClusterCli, ns2)
	if err != nil {
		return err
	}

	_, err = createNs(extraClusterCli, ns3)
	if err != nil {
		return err
	}

	_, err = createPVC(mainClusterCli, ns1, "source")
	if err != nil {
		return err
	}

	_, err = createPVC(mainClusterCli, ns1, "dest")
	if err != nil {
		return err
	}

	_, err = createPVC(mainClusterCli, ns2, "dest")
	if err != nil {
		return err
	}

	_, err = createPVC(extraClusterCli, ns3, "dest")
	if err != nil {
		return err
	}

	_, err = createPod(mainClusterCli, ns1, "source", "source")
	if err != nil {
		return err
	}

	_, err = createPod(mainClusterCli, ns1, "dest", "dest")
	if err != nil {
		return err
	}

	_, err = createPod(mainClusterCli, ns2, "dest", "dest")
	if err != nil {
		return err
	}

	_, err = createPod(extraClusterCli, ns3, "dest", "dest")
	if err != nil {
		return err
	}

	err = waitUntilPVCIsBound(mainClusterCli, ns1, "source")
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

	err = waitUntilPVCIsBound(extraClusterCli, ns3, "dest")
	if err != nil {
		return err
	}

	err = waitUntilPodIsRunning(mainClusterCli, ns1, "source")
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

	err = waitUntilPodIsRunning(extraClusterCli, ns3, "dest")
	if err != nil {
		return err
	}

	_, err = execInPod(mainClusterCli, ns1, "source", generateDataShellCommand)
	return err
}

func teardown() error {
	var result *multierror.Error
	err := deleteNs(mainClusterCli, ns1)
	if err != nil {
		result = multierror.Append(result, err)
	}
	err = deleteNs(mainClusterCli, ns2)
	if err != nil {
		result = multierror.Append(result, err)
	}
	err = deleteNs(extraClusterCli, ns3)
	if err != nil {
		result = multierror.Append(result, err)
	}
	return result.ErrorOrNil()
}

func userHomeDir() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	return usr.HomeDir, nil
}

func createPod(cli *k8s.ClusterClient, ns string, name string, pvc string) (*corev1.Pod, error) {
	terminationGracePeriodSeconds := int64(0)
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
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
	return cli.KubeClient.CoreV1().
		Pods(ns).Create(context.TODO(), &p, metav1.CreateOptions{})
}

func createPVC(cli *k8s.ClusterClient, ns string, name string) (*corev1.PersistentVolumeClaim, error) {
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
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

	return cli.KubeClient.CoreV1().PersistentVolumeClaims(ns).
		Create(context.TODO(), &pvc, metav1.CreateOptions{})
}

func waitUntilPodIsRunning(cli *k8s.ClusterClient, ns string, name string) error {
	resCli := cli.KubeClient.CoreV1().Pods(ns)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector
			return resCli.List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector
			return resCli.Watch(ctx, options)
		},
	}

	_, err := watchtools.UntilWithSync(ctx, lw, &corev1.Pod{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.Pod)
			if !ok {
				return false, fmt.Errorf("unexpected type while watcing pod %s/%s", ns, name)
			}

			return res.Status.Phase == corev1.PodRunning, nil
		})

	return err
}

func waitUntilPVCIsBound(cli *k8s.ClusterClient, ns string, name string) error {
	resCli := cli.KubeClient.CoreV1().PersistentVolumeClaims(ns)
	fieldSelector := fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fieldSelector
			return resCli.List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = fieldSelector
			return resCli.Watch(ctx, options)
		},
	}

	_, err := watchtools.UntilWithSync(ctx, lw, &corev1.PersistentVolumeClaim{}, nil,
		func(event watch.Event) (bool, error) {
			res, ok := event.Object.(*corev1.PersistentVolumeClaim)
			if !ok {
				return false, fmt.Errorf("unexpected type while watcing pvc %s/%s", ns, name)
			}

			return res.Status.Phase == corev1.ClaimBound, nil
		})

	return err
}

func execInPod(cli *k8s.ClusterClient, ns string, name string, cmd string) (string, error) {
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
		return "", err
	}

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", err
	}

	var result *multierror.Error
	err = exec.Stream(remotecommand.StreamOptions{Stdout: stdoutBuffer, Stderr: stderrBuffer})
	if err != nil {
		result = multierror.Append(result, err)
	}

	stdout := stdoutBuffer.String()
	stderr := stderrBuffer.String()

	if stderr != "" {
		result = multierror.Append(result, errors.New(stderr))
	}

	return stdout, result.ErrorOrNil()
}

func clearDests() error {
	_, err := execInPod(mainClusterCli, ns1, "dest", clearDataShellCommand)
	if err != nil {
		return err
	}
	_, err = execInPod(mainClusterCli, ns2, "dest", clearDataShellCommand)
	if err != nil {
		return err
	}

	_, err = execInPod(extraClusterCli, ns3, "dest", clearDataShellCommand)
	return err
}

func createNs(cli *k8s.ClusterClient, name string) (*corev1.Namespace, error) {
	return cli.KubeClient.CoreV1().
		Namespaces().Create(context.TODO(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}, metav1.CreateOptions{})
}

func deleteNs(cli *k8s.ClusterClient, name string) error {
	namespaces := cli.KubeClient.CoreV1().Namespaces()
	return namespaces.Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func runCliApp(cmd string) error {
	args := []string{os.Args[0]}
	args = append(args, strings.Fields(cmd)...)
	return app.New(log.New(), "", "").Run(args)
}
