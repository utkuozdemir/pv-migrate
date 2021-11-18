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
	"io/ioutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
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

	clusterClient *k8s.ClusterClient

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

	_, err := execInPod(ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug m -i -n %s -N %s source dest", ns1, ns1)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(ns1, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ns1, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestNoChown(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -i -o -n %s -N %s source dest", ns1, ns1)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(ns1, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, "0", parts[0])
	assert.Equal(t, "0", parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ns1, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestDeleteExtraneousFiles(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -d -i -n %s -N %s source dest", ns1, ns1)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(ns1, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ns1, "dest", checkExtraDataShellCommand)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "No such file or directory")
}

func TestMountedError(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(ns1, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -n %s -N %s source dest", ns1, ns1)
	err = runCliApp(cmd)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ignore-mounted is not requested")
}

func TestDifferentNS(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(ns2, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -i -n %s -N %s source dest", ns1, ns2)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(ns2, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ns2, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestRSA(t *testing.T) {
	assert.NoError(t, clearDests())

	_, err := execInPod(ns2, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -a rsa -i -n %s -N %s source dest", ns1, ns2)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(ns2, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ns2, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func TestDifferentCluster(t *testing.T) {
	assert.NoError(t, clearDests())

	usr, _ := user.Current()
	dir := usr.HomeDir
	kubeconfig := env.GetString("KUBECONFIG", dir+"/.kube/config")
	kubeconfigBytes, _ := ioutil.ReadFile(kubeconfig)
	kubeconfigCopyFile, _ := ioutil.TempFile("", "pv-migrate-test-config-*.yaml")
	kubeconfigCopy := kubeconfigCopyFile.Name()
	_ = ioutil.WriteFile(kubeconfigCopy, kubeconfigBytes, 0600)
	defer func() { _ = os.Remove(kubeconfigCopy) }()

	_, err := execInPod(ns2, "dest", generateExtraDataShellCommand)
	assert.NoError(t, err)

	cmd := fmt.Sprintf("-l debug -f json m -K %s -i -n %s -N %s source dest", kubeconfigCopy, ns1, ns2)
	assert.NoError(t, runCliApp(cmd))

	stdout, err := execInPod(ns2, "dest", printDataUidGidContentShellCommand)
	assert.NoError(t, err)

	parts := strings.Split(stdout, "\n")
	assert.Equal(t, len(parts), 3)
	if len(parts) < 3 {
		return
	}

	assert.Equal(t, dataFileUid, parts[0])
	assert.Equal(t, dataFileGid, parts[1])
	assert.Equal(t, generateDataContent, parts[2])

	_, err = execInPod(ns2, "dest", checkExtraDataShellCommand)
	assert.NoError(t, err)
}

func setup() error {
	client, err := k8s.GetClusterClient("", "")
	if err != nil {
		return err
	}

	clusterClient = client

	ns1 = "pv-migrate-test-1-" + util.RandomHexadecimalString(5)
	ns2 = "pv-migrate-test-2-" + util.RandomHexadecimalString(5)

	_, err = createNs(ns1)
	if err != nil {
		return err
	}

	_, err = createNs(ns2)
	if err != nil {
		return err
	}

	_, err = createPVC(ns1, "source")
	if err != nil {
		return err
	}

	_, err = createPVC(ns1, "dest")
	if err != nil {
		return err
	}

	_, err = createPVC(ns2, "dest")
	if err != nil {
		return err
	}

	_, err = createPod(ns1, "source", "source")
	if err != nil {
		return err
	}

	_, err = createPod(ns1, "dest", "dest")
	if err != nil {
		return err
	}

	_, err = createPod(ns2, "dest", "dest")
	if err != nil {
		return err
	}

	err = waitUntilPVCIsBound(ns1, "source")
	if err != nil {
		return err
	}

	err = waitUntilPVCIsBound(ns1, "dest")
	if err != nil {
		return err
	}

	err = waitUntilPVCIsBound(ns2, "dest")
	if err != nil {
		return err
	}

	err = waitUntilPodIsRunning(ns1, "source")
	if err != nil {
		return err
	}

	err = waitUntilPodIsRunning(ns1, "dest")
	if err != nil {
		return err
	}

	err = waitUntilPodIsRunning(ns2, "dest")
	if err != nil {
		return err
	}

	_, err = execInPod(ns1, "source", generateDataShellCommand)
	return err
}

func teardown() error {
	var result *multierror.Error
	err := deleteNs(ns1)
	if err != nil {
		result = multierror.Append(result, err)
	}
	err = deleteNs(ns2)
	if err != nil {
		result = multierror.Append(result, err)
	}
	return result.ErrorOrNil()
}

func createPod(ns string, name string, pvc string) (*corev1.Pod, error) {
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
	return clusterClient.KubeClient.CoreV1().
		Pods(ns).Create(context.TODO(), &p, metav1.CreateOptions{})
}

func createPVC(ns string, name string) (*corev1.PersistentVolumeClaim, error) {
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

	return clusterClient.KubeClient.CoreV1().PersistentVolumeClaims(ns).
		Create(context.TODO(), &pvc, metav1.CreateOptions{})
}

func waitUntilPodIsRunning(ns string, name string) error {
	watch, err := clusterClient.KubeClient.CoreV1().
		Pods(ns).Watch(context.TODO(), metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(metav1.ObjectNameField, name).String(),
	})
	if err != nil {
		return err
	}

	timeoutCh := time.After(1 * time.Minute)
	for {
		select {
		case event := <-watch.ResultChan():
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				return fmt.Errorf("unexpected type while watcing pvcs in ns %s", ns)
			}

			if pod.Name == name && pod.Status.Phase == corev1.PodRunning {
				return nil
			}
		case <-timeoutCh:
			return fmt.Errorf("timed out waiting for pod %s/%s to be running", ns, name)
		}
	}
}

func waitUntilPVCIsBound(ns string, name string) error {
	watch, err := clusterClient.KubeClient.CoreV1().
		PersistentVolumeClaims(ns).Watch(context.TODO(), metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(metav1.ObjectNameField, name).String(),
	})
	if err != nil {
		return err
	}

	timeoutCh := time.After(1 * time.Minute)
	for {
		select {
		case event := <-watch.ResultChan():
			pvc, ok := event.Object.(*corev1.PersistentVolumeClaim)
			if !ok {
				return fmt.Errorf("unexpected type while watcing pvcs in ns %s", ns)
			}

			if pvc.Name == name && pvc.Status.Phase == corev1.ClaimBound {
				return nil
			}
		case <-timeoutCh:
			return fmt.Errorf("timed out waiting for pvc %s/%s to be bound", ns, name)
		}
	}
}

func execInPod(ns string, name string, cmd string) (string, error) {
	stdoutBuffer := new(bytes.Buffer)
	stderrBuffer := new(bytes.Buffer)

	req := clusterClient.KubeClient.CoreV1().RESTClient().Post().Resource("pods").
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

	config, err := clusterClient.RESTClientGetter.ToRESTConfig()
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
	_, err := execInPod(ns1, "dest", clearDataShellCommand)
	if err != nil {
		return err
	}
	_, err = execInPod(ns2, "dest", clearDataShellCommand)
	return err
}

func createNs(name string) (*corev1.Namespace, error) {
	return clusterClient.KubeClient.CoreV1().
		Namespaces().Create(context.TODO(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}, metav1.CreateOptions{})
}

func deleteNs(name string) error {
	namespaces := clusterClient.KubeClient.CoreV1().Namespaces()
	return namespaces.Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func runCliApp(cmd string) error {
	args := []string{os.Args[0]}
	args = append(args, strings.Fields(cmd)...)
	return app.New(log.New(), "", "").Run(args)
}
