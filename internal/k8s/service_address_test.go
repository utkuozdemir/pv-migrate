package k8s_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
)

//nolint:paralleltest // the feature gate below is process-wide
func TestGetServiceAddressSkipsEmptyIngressUntilAddressArrives(t *testing.T) {
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, false)

	pendingPort := corev1.LoadBalancerIngress{
		Ports: []corev1.PortStatus{{Port: 22, Protocol: corev1.ProtocolTCP}},
	}
	initial := &corev1.Service{
		Namespace: "ns", Name: "lb", ResourceVersion: "1",
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{pendingPort},
			},
		},
	}

	updated := initial.DeepCopy()
	updated.ResourceVersion = "2"
	updated.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
		pendingPort,
		{IP: "203.0.113.10"},
	}

	cli := fake.NewClientset(initial)
	cli.PrependWatchReactor("services", func(_ k8stesting.Action) (bool, watch.Interface, error) {
		watcher := watch.NewRaceFreeFake()
		watcher.Modify(updated)

		return true, watcher, nil
	})

	addr, err := k8s.GetServiceAddress(t.Context(), cli, "ns", "lb", time.Second)
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.10", addr)
}
