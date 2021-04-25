package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/test"
	"github.com/utkuozdemir/pv-migrate/internal/util"
	"io/ioutil"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"regexp"
	"sigs.k8s.io/kind/pkg/cluster"
	"strings"
	"time"
)

const (
	testClusterName         = "pv-migrate-kind"
	pollInterval            = 5 * time.Second
	pollTimeout             = 5 * time.Minute
	kindWaitForReadyTimeout = 2 * time.Minute
)

var (
	//go:embed metallb-test.yaml
	metallbManifests string
	testContext      *kindTestContext

	//var predefinedKubeconfigPath = test.UserKubeconfigDir() + "/pv-migrate-test.yaml"
	predefinedKubeconfigPath = ""
)

type kindTestContext struct {
	clusterProvider *cluster.Provider
	kubeClient      kubernetes.Interface
	config          *restclient.Config
	kubeconfig      string
}

func setupMetalLB(kubeClient kubernetes.Interface, config *restclient.Config) error {
	err := applyMetalLBManifests(config)
	if err != nil {
		return err
	}
	return createMetalLBConfig(kubeClient)
}

func applyMetalLBManifests(config *restclient.Config) error {
	dc, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return err
	}

	regex := regexp.MustCompile(`---\s*`)
	manifests := regex.Split(metallbManifests, -1)
	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	for _, manifest := range manifests {
		obj := &unstructured.Unstructured{}
		_, gvk, err := dec.Decode([]byte(manifest), nil, obj)
		if err != nil {
			return err
		}

		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		var dr dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			dr = dyn.Resource(mapping.Resource).Namespace(obj.GetNamespace())
		} else {
			dr = dyn.Resource(mapping.Resource)
		}

		data, err := json.Marshal(obj)
		if err != nil {
			return err
		}

		_, err = dr.Patch(context.TODO(), obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
			FieldManager: "pv-migrate-test-controller",
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func createMetalLBConfig(kubeClient kubernetes.Interface) error {
	nodeList, err := kubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	nodeInternalIP, err := getNodeInternalIP(&nodeList.Items[0])
	if err != nil {
		return err
	}
	nodeIPParts := strings.Split(nodeInternalIP, ".")
	rangeStart := fmt.Sprintf("%s.%s.255.200", nodeIPParts[0], nodeIPParts[1])
	rangeEnd := fmt.Sprintf("%s.%s.255.250", nodeIPParts[0], nodeIPParts[1])

	metalLBConfig := fmt.Sprintf(`{"address-pools":[{"name":"default","protocol":"layer2","addresses":["%s-%s"]}]}`,
		rangeStart, rangeEnd)

	configMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: "metallb-system",
		},
		Data: map[string]string{
			"config": metalLBConfig,
		},
	}

	_, err = kubeClient.CoreV1().ConfigMaps(configMap.Namespace).Create(context.TODO(), &configMap, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}

func getNodeInternalIP(node *corev1.Node) (string, error) {
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			return address.Address, nil
		}
	}
	return "", errors.New("no internal ip found on node")
}

func beforeTests() {
	cli.OsExiter = func(code int) {
		// preventing urfave/cli to call os.Exit
		log.WithField("code", code).Info("os.Exit is called")
	}
	kindTestContext, err := setupKindCluster(testClusterName, "kindest/node:v1.20.2")
	testContext = kindTestContext
	if err != nil {
		log.WithError(err).Error("failed to setup kind cluster")
		if //goland:noinspection GoBoolExpressions
		predefinedKubeconfigPath == "" {
			err := destroyKindCluster(kindTestContext.clusterProvider, testClusterName, kindTestContext.kubeconfig)
			if err != nil {
				log.WithError(err).Error("failed to destroy kind cluster")
			}
			os.Exit(1)
		}
	}
}

func afterTests() {
	//goland:noinspection GoBoolExpressions
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

	//goland:noinspection GoBoolExpressions
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
			cluster.CreateWithWaitForReady(kindWaitForReadyTimeout),
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

	pvcAaaAaa := test.PVC(test.ObjectMeta("aaa", "aaa"), "512Mi", corev1.ReadWriteOnce)
	err = createAndBindPVC(kubeClient, *pvcAaaAaa)
	if err != nil {
		return err
	}

	pvcAaaBbb := test.PVC(test.ObjectMeta("aaa", "bbb"), "1024Mi", corev1.ReadWriteOnce)
	err = createAndBindPVC(kubeClient, *pvcAaaBbb)
	if err != nil {
		return err
	}

	pvcAaaCcc := test.PVC(test.ObjectMeta("aaa", "ccc"), "1024Mi", corev1.ReadWriteOnce)
	err = createAndBindPVC(kubeClient, *pvcAaaCcc)
	if err != nil {
		return err
	}

	pvcBbbBbb := test.PVC(test.ObjectMeta("bbb", "bbb"), "1024Mi", corev1.ReadWriteOnce)
	err = createAndBindPVC(kubeClient, *pvcBbbBbb)
	if err != nil {
		return err
	}

	pvcCccCcc := test.PVC(test.ObjectMeta("ccc", "ccc"), "1024Mi", corev1.ReadWriteOnce)
	err = createAndBindPVC(kubeClient, *pvcCccCcc)
	if err != nil {
		return err
	}

	return nil
}

func createNS(kubeClient kubernetes.Interface, namespace corev1.Namespace) error {
	_, err := kubeClient.CoreV1().Namespaces().Create(context.TODO(), &namespace, metav1.CreateOptions{})
	return err
}

func createAndBindPVC(kubeClient kubernetes.Interface, pvc corev1.PersistentVolumeClaim) error {
	_, err := kubeClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(context.TODO(), &pvc, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	return ensurePVCIsBound(kubeClient, pvc.Namespace, pvc.Name)
}

func destroyKindCluster(clusterProvider *cluster.Provider, name string, kubeconfig string) error {
	err := clusterProvider.Delete(name, kubeconfig)
	if err != nil {
		return err
	}

	//goland:noinspection GoBoolExpressions
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

func execInPodWithPVC(namespace string, pvcName string, command []string) (string, string, error) {
	kubeClient := testContext.kubeClient
	pod, err := startPodWithPVCMount(kubeClient, namespace, pvcName)
	if err != nil {
		return "", "", err
	}

	defer func() {
		_ = ensurePodIsDeleted(kubeClient, namespace, pod.Name)
	}()

	log.WithField("namespace", namespace).
		WithField("pod", pod.Name).
		WithField("command", strings.Join(command, " ")).
		Info("Executing command in pod")
	return k8s.ExecInPod(kubeClient, testContext.config, namespace, pod.Name, command)
}

func waitUntilPVCIsBound(kubeClient kubernetes.Interface, namespace string, name string) error {
	logger := log.WithField("namespace", namespace).WithField("name", name)
	return wait.PollImmediate(pollInterval, pollTimeout, func() (done bool, err error) {
		phase, err := getPVCPhase(kubeClient, namespace, name)
		if err != nil {
			return false, err
		}

		done = *phase == corev1.ClaimBound
		logger.WithField("phase", phase).Info("Still not bound, polling...")
		return done, err
	})
}

func getPVCPhase(kubeClient kubernetes.Interface, namespace string, name string) (*corev1.PersistentVolumeClaimPhase, error) {
	pvc, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return &pvc.Status.Phase, nil
}

func ensurePVCIsBound(kubeClient kubernetes.Interface, namespace string, name string) error {
	pvc, err := kubeClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil
	}

	if pvc.Status.Phase == corev1.ClaimBound {
		return nil
	}

	terminationGracePeriodSeconds := int64(0)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("pv-migrate-binder-%s-%s", name, util.RandomHexadecimalString(5)),
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
			Volumes: []corev1.Volume{
				{
					Name: "volume",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: name,
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

	podLogger := log.WithField("namespace", pod.Namespace).WithField("pod", pod.Name)
	podLogger.Info("Creating binder pod")
	_, err = kubeClient.CoreV1().Pods(namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	defer func() {
		_ = ensurePodIsDeleted(kubeClient, pod.Namespace, pod.Name)
	}()

	return waitUntilPVCIsBound(kubeClient, namespace, name)
}

func ensurePodIsDeleted(kubeClient kubernetes.Interface, namespace string, name string) error {
	podLogger := log.WithFields(log.Fields{
		"namespace": namespace,
		"pod":       name,
	})

	podLogger.Info("Deleting pod")
	err := kubeClient.CoreV1().Pods(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if kubeerrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	return wait.PollImmediate(pollInterval, pollTimeout, func() (done bool, err error) {
		_, err = kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if kubeerrors.IsNotFound(err) {
			return true, nil
		}

		if err != nil {
			return false, err
		}

		podLogger.Info("Pod is still there, polling...")
		return false, nil
	})
}

func startPodWithPVCMount(kubeClient kubernetes.Interface, namespace string, pvcName string) (*corev1.Pod, error) {
	terminationGracePeriodSeconds := int64(0)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("pv-migrate-binder-%s-%s", pvcName, util.RandomHexadecimalString(5)),
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
			Volumes: []corev1.Volume{
				{
					Name: "volume",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
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

	log.WithFields(
		log.Fields{
			"namespace": pod.Namespace,
			"pod":       pod.Name,
			"pvc":       pvcName,
		}).Info("Creating pod with PVC mount")
	created, err := kubeClient.CoreV1().Pods(namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	err = wait.PollImmediate(pollInterval, pollTimeout, func() (done bool, err error) {
		phase, err := getPodPhase(kubeClient, namespace, pod.Name)
		if err != nil {
			return false, err
		}

		running := *phase == corev1.PodRunning
		return running, nil
	})

	return created, err
}

func getPodPhase(kubeClient kubernetes.Interface, namespace string, name string) (*corev1.PodPhase, error) {
	pod, err := kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return &pod.Status.Phase, nil
}
