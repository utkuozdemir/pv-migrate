package k8s

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
	"k8s.io/client-go/kubernetes/fake"
)

// TestFindNodePort tests the findNodePort helper function.
func TestFindNodePort(t *testing.T) {
	t.Parallel()

	// Test with SSH port and various service configurations
	testFindNodePortWithSSH(t)

	// Test fallback to first port when no SSH port is available
	testFindNodePortFallback(t)

	// Test service with no ports
	testFindNodePortEmptyPorts(t)
}

// testFindNodePortWithSSH tests finding SSH ports in services.
func testFindNodePortWithSSH(t *testing.T) {
	t.Helper()

	// Test with SSH port by name
	svcWithSSH := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					NodePort: 30080,
				},
				{
					Name:     "ssh",
					Port:     22,
					NodePort: 32222,
				},
			},
		},
	}

	port, err := findNodePort(svcWithSSH)
	require.NoError(t, err)
	assert.Equal(t, 32222, port, "Should select the SSH port")

	// Test with port 22 but different name
	svcWithPort22 := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					NodePort: 30080,
				},
				{
					Name:     "custom",
					Port:     22,
					NodePort: 32222,
				},
			},
		},
	}

	port, err = findNodePort(svcWithPort22)
	require.NoError(t, err)
	assert.Equal(t, 32222, port, "Should select port 22 even with different name")
}

// testFindNodePortFallback tests fallback to first port.
func testFindNodePortFallback(t *testing.T) {
	t.Helper()

	svcWithoutSSH := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					NodePort: 30080,
				},
				{
					Name:     "https",
					Port:     443,
					NodePort: 30443,
				},
			},
		},
	}

	port, err := findNodePort(svcWithoutSSH)
	require.NoError(t, err)
	assert.Equal(t, 30080, port, "Should fallback to first port")
}

// testFindNodePortEmptyPorts tests service with no ports.
func testFindNodePortEmptyPorts(t *testing.T) {
	t.Helper()

	svcNoPort := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{},
		},
	}

	_, err := findNodePort(svcNoPort)
	assert.Error(t, err, "Should return error for service with no ports")
}

func TestGetNodeIP(t *testing.T) {
	t.Parallel()

	fakeClient := fake.NewClientset(
		&corev1.Node{
			Name: "node1",
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "192.168.1.100"},
				},
			},
		},
		&corev1.Node{
			Name: "node2",
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeHostName, Address: "worker-node2"},
				},
			},
		},
	)

	ctx := t.Context()

	ip, err := GetNodeIP(ctx, fakeClient, "node1")
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.100", ip)

	_, err = GetNodeIP(ctx, fakeClient, "node2")
	require.Error(t, err, "node with only hostname should fail")

	_, err = GetNodeIP(ctx, fakeClient, "nonexistent")
	require.Error(t, err, "nonexistent node should fail")
}

func TestGetAnyNodeIP(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	// No nodes
	empty := fake.NewClientset()
	_, err := GetAnyNodeIP(ctx, empty)
	require.Error(t, err)

	// Mixed nodes — should find the one with a usable IP
	mixed := fake.NewClientset(
		&corev1.Node{
			Name: "node1",
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeHostName, Address: "worker-node1"},
				},
			},
		},
		&corev1.Node{
			Name: "node2",
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeExternalIP, Address: "10.0.0.1"},
				},
			},
		},
	)

	ip, err := GetAnyNodeIP(ctx, mixed)
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", ip)
}

// A LoadBalancer Service carries a node port too, and the loadbalancer strategy
// falls back to it, so the lookup accepts that type. One created without node
// ports is an error rather than port zero.
//
// Not parallel: the feature gate is process-wide. The streaming list the
// reflector prefers is not served by the fake clientset.
//
//nolint:paralleltest
func TestGetNodePortOfLoadBalancerService(t *testing.T) {
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, false)

	withPort := &corev1.Service{
		Namespace: "ns", Name: "with-port",
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Name: "ssh", Port: 22, NodePort: 31234}},
		},
	}
	withoutPort := &corev1.Service{
		Namespace: "ns", Name: "without-port",
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Name: "ssh", Port: 22}},
		},
	}
	clusterIP := &corev1.Service{
		Namespace: "ns", Name: "cluster-ip",
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Name: "ssh", Port: 22}},
		},
	}

	// One client per Service: the fake clientset does not apply the field
	// selector the lookup watches with, so a shared one would deliver them all.
	port, err := GetNodePort(t.Context(), fake.NewClientset(withPort), "ns", "with-port", time.Second)
	require.NoError(t, err)
	assert.Equal(t, 31234, port)

	_, err = GetNodePort(t.Context(), fake.NewClientset(withoutPort), "ns", "without-port", time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no node port allocated")

	_, err = GetNodePort(t.Context(), fake.NewClientset(clusterIP), "ns", "cluster-ip", time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no node port")
}
