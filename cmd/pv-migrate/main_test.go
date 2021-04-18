package main

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v2"
	"github.com/utkuozdemir/pv-migrate/internal/app"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/test"
	"io/ioutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"sigs.k8s.io/kind/pkg/cluster"
	"strings"
	"testing"
	"time"
)

const testClusterName = "pv-migrate-kind"
const pollInterval = 5 * time.Second
const pollTimeout = 5 * time.Minute

var testContext *kindTestContext

//var predefinedKubeconfigPath = test.UserKubeconfigDir() + "/pv-migrate-test.yaml"
var predefinedKubeconfigPath = ""

type kindTestContext struct {
	clusterProvider *cluster.Provider
	kubeClient      kubernetes.Interface
	config          *restclient.Config
	kubeconfig      string
}

func TestMain(m *testing.M) {
	before()
	code := m.Run()
	after()
	os.Exit(code)
}

func before() {
	cli.OsExiter = func(code int) {
		// preventing urfave/cli to call os.Exit
		log.WithField("code", code).Info("os.Exit is called")
	}
	kindTestContext, err := setupKindCluster(testClusterName, "kindest/node:v1.20.2")
	testContext = kindTestContext
	if err != nil {
		log.WithError(err).Error("failed to setup kind cluster")
		if predefinedKubeconfigPath == "" {
			err := destroyKindCluster(kindTestContext.clusterProvider, testClusterName, kindTestContext.kubeconfig)
			if err != nil {
				log.WithError(err).Error("failed to destroy kind cluster")
			}
			os.Exit(1)
		}
	}
}

func after() {
	if predefinedKubeconfigPath == "" {
		err := destroyKindCluster(testContext.clusterProvider, testClusterName, testContext.kubeconfig)
		if err != nil {
			log.WithError(err).Error("failed to destroy kind cluster")
			os.Exit(1)
		}
	}
}

func setupKindCluster(name string, nodeImage string) (*kindTestContext, error) {
	testContext := kindTestContext{}

	kubeconfig := predefinedKubeconfigPath

	if kubeconfig == "" {
		log.Info("there is no predefined kubeconfig path, will create a temporary one")
		kubeconfigFile, err := ioutil.TempFile("", "pv-migrate-kind-config-*.yaml")
		if err != nil {
			return &testContext, err
		}
		kubeconfig = kubeconfigFile.Name()
	}

	testContext.kubeconfig = kubeconfig
	logWithKubeconfig := log.WithField("kubeconfig", kubeconfig)
	logWithKubeconfig.Infof("using kubeconfig for kind cluster")

	clusterProvider := cluster.NewProvider()
	testContext.clusterProvider = clusterProvider

	if !isAccessibleCluster(kubeconfig) {
		logWithKubeconfig.Info("no accessible kind cluster found, will create one")
		err := clusterProvider.Create(
			name,
			cluster.CreateWithKubeconfigPath(kubeconfig),
			cluster.CreateWithNodeImage(nodeImage),
			cluster.CreateWithDisplayUsage(true),
			cluster.CreateWithDisplaySalutation(true),
		)

		if err != nil {
			return &testContext, err
		}
	}

	kubeClient, config, err := initKubeClient(kubeconfig)
	testContext.kubeClient = kubeClient
	testContext.config = config

	if err != nil {
		return &testContext, err
	}

	logWithCluster := logWithKubeconfig.WithField("cluster", testClusterName)
	logWithCluster.Info("will set up metallb")
	err = setupMetalLB(kubeClient, config)
	if err != nil {
		return &testContext, err
	}

	logWithCluster.Info("will apply the initial resources")
	err = createK8sResources(kubeClient)
	return &testContext, err
}

func isAccessibleCluster(kubeconfig string) bool {
	client, _, err := initKubeClient(kubeconfig)
	if err != nil {
		return false
	}
	_, err = client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	return err == nil
}

//func waitUntilAllPodsAreReady(kubeClient kubernetes.Interface, namespace string) error {
//	pods, err := kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
//	if err != nil {
//		return err
//	}
//
//	for _, pod := range pods.Items {
//		err := waitUntilPodIsReady(kubeClient, namespace, pod.Name)
//		if err != nil {
//			return err
//		}
//	}
//
//	return nil
//}

func createK8sResources(kubeClient kubernetes.Interface) error {
	err := createNS(kubeClient, test.NS("aaa"))
	if err != nil {
		return err
	}
	err = createNS(kubeClient, test.NS("bbb"))
	if err != nil {
		return err
	}
	err = createNS(kubeClient, test.NS("ccc"))
	if err != nil {
		return err
	}

	depAaaAaa := test.Deployment(test.ObjectMeta("aaa", "aaa"), "aaa")
	err = createDeployment(kubeClient, depAaaAaa)
	if err != nil {
		return err
	}

	depAaaBbb := test.Deployment(test.ObjectMeta("aaa", "bbb"), "bbb")
	err = createDeployment(kubeClient, depAaaBbb)
	if err != nil {
		return err
	}

	depBbbBbb := test.Deployment(test.ObjectMeta("bbb", "bbb"), "bbb")
	err = createDeployment(kubeClient, depBbbBbb)
	if err != nil {
		return err
	}

	depCccCcc := test.Deployment(test.ObjectMeta("ccc", "ccc"), "ccc")
	err = createDeployment(kubeClient, depCccCcc)
	if err != nil {
		return err
	}

	pvcAaaAaa := test.PVC(test.ObjectMeta("aaa", "aaa"), "512Mi", corev1.ReadWriteOnce)
	err = createPVC(kubeClient, *pvcAaaAaa)
	if err != nil {
		return err
	}

	pvcAaaBbb := test.PVC(test.ObjectMeta("aaa", "bbb"), "1024Mi", corev1.ReadWriteOnce)
	err = createPVC(kubeClient, *pvcAaaBbb)
	if err != nil {
		return err
	}

	pvcBbbBbb := test.PVC(test.ObjectMeta("bbb", "bbb"), "1024Mi", corev1.ReadWriteOnce)
	err = createPVC(kubeClient, *pvcBbbBbb)
	if err != nil {
		return err
	}

	pvcCccCcc := test.PVC(test.ObjectMeta("ccc", "ccc"), "1024Mi", corev1.ReadWriteOnce)
	err = createPVC(kubeClient, *pvcCccCcc)
	if err != nil {
		return err
	}

	return nil
}

func createNS(kubeclient kubernetes.Interface, namespace corev1.Namespace) error {
	_, err := kubeclient.CoreV1().Namespaces().Create(context.TODO(), &namespace, metav1.CreateOptions{})
	return err
}

func createPVC(kubeClient kubernetes.Interface, pvc corev1.PersistentVolumeClaim) error {
	_, err := kubeClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(context.TODO(), &pvc, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return waitUntilPVCIsBound(kubeClient, pvc.Namespace, pvc.Name)
}

func createDeployment(kubeClient kubernetes.Interface, dep *appsv1.Deployment) error {
	_, err := kubeClient.AppsV1().Deployments(dep.Namespace).Create(context.TODO(), dep, metav1.CreateOptions{})
	return err
}

func destroyKindCluster(clusterProvider *cluster.Provider, name string, kubeconfig string) error {
	err := clusterProvider.Delete(name, kubeconfig)
	if err != nil {
		return err
	}

	if predefinedKubeconfigPath == "" {
		return os.Remove(kubeconfig)
	}
	return nil
}

func initKubeClient(kubeconfig string) (kubernetes.Interface, *restclient.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, config, err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, config, err
	}

	return client, config, nil
}

func execInFirstPodWithPrefix(namespace string, prefix string, command []string) (string, string, error) {
	podName, err := getFirstPodNameWithPrefix(testContext.kubeClient, namespace, prefix)
	log.WithField("namespace", namespace).
		WithField("pod", podName).
		WithField("command", strings.Join(command, " ")).
		Info("executing command in pod")
	if err != nil {
		return "", "", err
	}

	return k8s.ExecInPod(testContext.kubeClient, testContext.config,
		namespace, podName, command)
}

func getFirstPodNameWithPrefix(kubeClient kubernetes.Interface, namespace string, prefix string) (string, error) {
	podList, err := kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, pod := range podList.Items {
		if strings.HasPrefix(pod.Name, prefix) {
			return pod.Name, nil
		}
	}

	return "", fmt.Errorf("no pod with prefix %s", prefix)
}

func waitUntilPVCIsBound(kubeClient kubernetes.Interface, namespace string, name string) error {
	return wait.PollImmediate(pollInterval, pollTimeout, func() (done bool, err error) {
		bound, err := isPVCBound(kubeClient, namespace, name)
		if err != nil {
			return true, err
		}

		if bound {
			return true, nil
		}

		return false, nil
	})
}

func isPVCBound(kubeClient kubernetes.Interface, namespace string, name string) (bool, error) {
	pvc, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return false, nil
	}

	return pvc.Status.Phase == corev1.ClaimBound, nil
}

func TestSameNS(t *testing.T) {
	cliApp := app.Build()

	args := []string{
		os.Args[0], "migrate",
		"--source-kubeconfig", testContext.kubeconfig,
		"--source-namespace", "aaa",
		"--dest-kubeconfig", testContext.kubeconfig,
		"--dest-namespace", "aaa",
		"aaa", "bbb",
	}

	_, _, err := execInFirstPodWithPrefix("aaa", "aaa",
		[]string{"sh", "-c", "echo -n aaaaa > /volume/file.txt"})
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInFirstPodWithPrefix("aaa", "bbb",
		[]string{"cat", "/volume/file.txt"})
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, "aaaaa", stdout)
}

func TestDifferentNS(t *testing.T) {
	cliApp := app.Build()

	args := []string{
		os.Args[0], "migrate",
		"--source-kubeconfig", testContext.kubeconfig,
		"--source-namespace", "aaa",
		"--dest-kubeconfig", testContext.kubeconfig,
		"--dest-namespace", "bbb",
		"aaa", "bbb",
	}

	_, _, err := execInFirstPodWithPrefix("aaa", "aaa",
		[]string{"sh", "-c", "echo -n DATA > /volume/file.txt"})
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInFirstPodWithPrefix("bbb", "bbb",
		[]string{"cat", "/volume/file.txt"})
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, "DATA", stdout)
}

// TestDifferentCluster will trick the application to "think" that source and dest are in 2 different clusters
// while actually both of them being in the same "kind cluster".
func TestDifferentCluster(t *testing.T) {
	kubeconfigBytes, _ := ioutil.ReadFile(testContext.kubeconfig)
	kubeconfigCopyFile, _ := ioutil.TempFile("", "pv-migrate-kind-config-*.yaml")
	kubeconfigCopy := kubeconfigCopyFile.Name()
	_ = ioutil.WriteFile(kubeconfigCopy, kubeconfigBytes, 0600)
	defer func() { _ = os.Remove(kubeconfigCopy) }()

	cliApp := app.Build()

	args := []string{
		os.Args[0], "migrate",
		"--source-kubeconfig", testContext.kubeconfig,
		"--source-namespace", "aaa",
		"--dest-kubeconfig", kubeconfigCopy,
		"--dest-namespace", "ccc",
		"aaa", "ccc",
	}

	_, _, err := execInFirstPodWithPrefix("aaa", "aaa",
		[]string{"sh", "-c", "echo -n DATA > /volume/file.txt"})
	assert.NoError(t, err)

	err = cliApp.Run(args)
	assert.NoError(t, err)

	stdout, stderr, err := execInFirstPodWithPrefix("ccc", "ccc",
		[]string{"cat", "/volume/file.txt"})
	assert.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Equal(t, "DATA", stdout)
}

//func waitUntilPodIsReady(kubeClient kubernetes.Interface, namespace string, name string) error {
//	return wait.PollImmediate(pollInterval, pollTimeout, func() (done bool, err error) {
//		ready, err := isPodReady(kubeClient, namespace, name)
//		if err != nil {
//			return true, err
//		}
//
//		if ready {
//			return true, nil
//		}
//
//		return false, nil
//	})
//}

//func isPodReady(kubeClient kubernetes.Interface, namespace string, name string) (bool, error) {
//	pod, err := kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
//	if err != nil {
//		return false, err
//	}
//
//	for _, condition := range pod.Status.Conditions {
//		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
//			return true, nil
//		}
//	}
//
//	return false, nil
//}
