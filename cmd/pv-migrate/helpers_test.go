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
	"io/ioutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
	testClusterName = "pv-migrate-kind"
	pollInterval    = 5 * time.Second
	pollTimeout     = 5 * time.Minute
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
