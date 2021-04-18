package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
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
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"regexp"
	"strings"
)

//go:embed metallb-test.yaml
var metallbManifests string

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
