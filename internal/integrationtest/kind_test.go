package integrationtest

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"io/ioutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
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
	//go:embed _metallb-test.yaml
	metallbManifests string
)

type pvMigrateTestContext struct {
	clusterProvider *cluster.Provider
	kubeClient      kubernetes.Interface
	config          *rest.Config
	kubeconfig      string
}

func setupMetalLB(kubeClient kubernetes.Interface, config *rest.Config) error {
	err := applyMetalLBManifests(config)
	if err != nil {
		return err
	}
	return createMetalLBConfig(kubeClient)
}

func applyMetalLBManifests(config *rest.Config) error {
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

func createKindTestContext() *pvMigrateTestContext {
	cli.OsExiter = func(code int) {
		// preventing urfave/cli to call os.Exit
		log.WithField("code", code).Info("os.Exit is called")
	}
	kindTestContext, err := setupKindCluster(testClusterName, "kindest/node:v1.20.2")
	if err != nil {
		log.WithError(err).Error("failed to setup kind cluster")
		err := destroyKindCluster(kindTestContext.clusterProvider, kindTestContext.kubeconfig)
		if err != nil {
			log.WithError(err).Error("failed to destroy kind cluster")
		}
		os.Exit(1)
	}
	return kindTestContext
}

func setupKindCluster(name string, nodeImage string) (*pvMigrateTestContext, error) {
	testContext := pvMigrateTestContext{}

	log.Info("there is no predefined kubeconfig path, will create a temporary one")
	kubeconfigFile, err := ioutil.TempFile("", "pv-migrate-kind-config-*.yaml")
	if err != nil {
		return &testContext, err
	}
	kubeconfig := kubeconfigFile.Name()

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

	kubeClient, config, err := buildKubeClient(kubeconfig)
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
	return &testContext, err
}

func isAccessibleCluster(kubeconfig string) bool {
	client, _, err := buildKubeClient(kubeconfig)
	if err != nil {
		return false
	}
	_, err = client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	return err == nil
}

func destroyKindCluster(clusterProvider *cluster.Provider, kubeconfig string) error {
	err := clusterProvider.Delete(testClusterName, kubeconfig)
	if err != nil {
		return err
	}

	return os.Remove(kubeconfig)
}
