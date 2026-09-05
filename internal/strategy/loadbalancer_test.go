package strategy

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/neilotoole/slogt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
)

func TestFormatSSHTargetHost(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1.2.3.4", formatSSHTargetHost("1.2.3.4"))
	assert.Equal(t, "example.com", formatSSHTargetHost("example.com"))
	assert.Equal(t, "[2001:0db8:85a3:0000:0000:8a2e:0370:7334]",
		formatSSHTargetHost("2001:0db8:85a3:0000:0000:8a2e:0370:7334"))
	assert.Equal(t, "[::1]", formatSSHTargetHost("::1"))
}

// TestFormatSSHTargetHostIsIdempotent is what lets --dest-host-override go
// through the same formatting as a resolved address. The override used to replace
// the resolved host after it had been bracketed, so an IPv6 literal passed to it
// reached rsync unbracketed, and rsync split the remote spec on the literal's own
// first colon and looked for a host called "fe80".
func TestFormatSSHTargetHostIsIdempotent(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"1.2.3.4", "example.com", "::1", "[::1]", "fe80::1%eth0", ""} {
		once := formatSSHTargetHost(host)
		assert.Equal(t, once, formatSSHTargetHost(once),
			"formatting %q twice must not differ from formatting it once", host)
	}
}

// The fixtures below stand in for what the loadbalancer strategy finds after
// its sshd release is installed: the Service, the sshd pod, and the pod's node.

func lbFixtures(nodePort int32, ingress []corev1.LoadBalancerIngress) (*pvc.Info, *migration.Attempt) {
	return lbFixturesWithPod(nodePort, ingress, true)
}

func lbFixturesWithPod(
	nodePort int32, ingress []corev1.LoadBalancerIngress, podReady bool,
) (*pvc.Info, *migration.Attempt) {
	const release = "pv-migrate-test-loadbalancer-src"

	podConditions := []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}
	if podReady {
		podConditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	}

	svc := &corev1.Service{
		Namespace: "ns", Name: release + "-sshd",
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Name: "ssh", Port: 22, NodePort: nodePort}},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: ingress}},
	}
	pod := &corev1.Pod{
		Namespace: "ns",
		Name:      release + "-sshd-abc12",
		Labels: map[string]string{
			"app.kubernetes.io/component": "sshd",
			"app.kubernetes.io/instance":  release,
		},
		Spec:   corev1.PodSpec{NodeName: "worker-1"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: podConditions},
	}
	node := &corev1.Node{
		Name: "worker-1",
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.7"}},
		},
	}

	info := &pvc.Info{
		ClusterClient: &k8s.ClusterClient{KubeClient: fake.NewClientset(svc, pod, node)},
		Claim:         &corev1.PersistentVolumeClaim{Namespace: "ns", Name: "src"},
	}
	attempt := &migration.Attempt{
		HelmReleaseNamePrefix: "pv-migrate-test-loadbalancer",
		Migration: &migration.Migration{
			Request: &migration.Request{
				HelmTimeout:         time.Minute,
				LoadBalancerTimeout: 10 * time.Millisecond,
				Writer:              io.Discard,
			},
			SourceInfo: info,
		},
	}

	return info, attempt
}

// Not parallel: the feature gate is process-wide. The streaming list the
// reflector prefers is not served by the fake clientset.
//
//nolint:paralleltest
func TestResolveLBTarget(t *testing.T) {
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, false)

	t.Run("uses the address", func(t *testing.T) {
		info, attempt := lbFixtures(31234, []corev1.LoadBalancerIngress{{IP: "203.0.113.9"}})
		topo := topology{sshd: componentSide{info: info}}

		target, err := resolveLBTarget(t.Context(), attempt, topo, attempt.HelmReleaseNamePrefix+"-src", slogt.New(t))
		require.NoError(t, err)

		assert.Equal(t, sshTarget{host: "203.0.113.9"}, target, "with an address the node port is not used")
	})

	t.Run("falls back to the node port", func(t *testing.T) {
		info, attempt := lbFixtures(31234, nil)
		topo := topology{sshd: componentSide{info: info}}

		target, err := resolveLBTarget(t.Context(), attempt, topo, attempt.HelmReleaseNamePrefix+"-src", slogt.New(t))
		require.NoError(t, err)

		assert.Equal(t, sshTarget{host: "10.0.0.7", port: 31234}, target,
			"a pending load balancer falls back to the node port on the sshd pod's node")
	})
}

// Not parallel, for the same reason as above.
//
//nolint:paralleltest
func TestResolveLBTarget_Waits(t *testing.T) {
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, false)

	t.Run("pod not ready within the helm timeout", func(t *testing.T) {
		info, attempt := lbFixturesWithPod(31234, nil, false)
		attempt.Migration.Request.HelmTimeout = 20 * time.Millisecond
		topo := topology{sshd: componentSide{info: info}}

		_, err := resolveLBTarget(t.Context(), attempt, topo, attempt.HelmReleaseNamePrefix+"-src", slogt.New(t))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--helm-timeout",
			"the sshd pod wait stands in for Helm's wait, so it has Helm's budget and names its flag")
	})

	t.Run("host override skips the node lookup", func(t *testing.T) {
		info, attempt := lbFixtures(31234, nil)
		attempt.Migration.Request.DestHostOverride = "gateway.example.com"
		topo := topology{sshd: componentSide{info: info}}

		target, err := resolveLBTarget(t.Context(), attempt, topo, attempt.HelmReleaseNamePrefix+"-src", slogt.New(t))
		require.NoError(t, err)

		assert.Equal(t, sshTarget{port: 31234}, target, "only the port is resolved, the host comes from the override")

		fakeClient, ok := info.ClusterClient.KubeClient.(*fake.Clientset)
		require.True(t, ok)

		for _, action := range fakeClient.Actions() {
			assert.NotEqual(t, "nodes", action.GetResource().Resource, "no node is read when the host is given")
		}
	})

	t.Run("an API error is not a missing address", func(t *testing.T) {
		info, attempt := lbFixtures(31234, nil)
		topo := topology{sshd: componentSide{info: info}}

		fakeClient, ok := info.ClusterClient.KubeClient.(*fake.Clientset)
		require.True(t, ok)

		fakeClient.PrependReactor("get", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("services is forbidden")
		})

		_, err := resolveLBTarget(t.Context(), attempt, topo, attempt.HelmReleaseNamePrefix+"-src", slogt.New(t))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "forbidden", "the API's own error is reported")

		for _, action := range fakeClient.Actions() {
			assert.NotEqual(t, "nodes", action.GetResource().Resource, "an API error must not fall back")
		}
	})

	t.Run("no node port to fall back to", func(t *testing.T) {
		info, attempt := lbFixtures(0, nil)
		topo := topology{sshd: componentSide{info: info}}

		_, err := resolveLBTarget(t.Context(), attempt, topo, attempt.HelmReleaseNamePrefix+"-src", slogt.New(t))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no node port allocated",
			"a Service created without node ports has nothing to fall back to, and the error says so")
	})
}

// TestInstallsLoadBalancer pins the check that decides whether Helm waits for a
// release, against the values the strategies actually build, so the two sides
// cannot drift apart and quietly bring the wait on the address back.
func TestInstallsLoadBalancer(t *testing.T) {
	t.Parallel()

	info := &pvc.Info{Claim: &corev1.PersistentVolumeClaim{Namespace: "ns", Name: "pvc"}}
	side := componentSide{info: info, mountPath: srcMountPath, readOnly: true}

	assert.True(t, installsLoadBalancer(sshdReleaseValues(side, "ssh-ed25519 AAAA", "LoadBalancer")))
	assert.False(t, installsLoadBalancer(sshdReleaseValues(side, "ssh-ed25519 AAAA", "NodePort")))
	assert.False(t, installsLoadBalancer(sshdReleaseValues(side, "ssh-ed25519 AAAA", "ClusterIP")))
	assert.False(t, installsLoadBalancer(map[string]any{sshdComponent: buildSshdHelmValues(side, "key")}),
		"no service section means no load balancer")
	assert.False(t, installsLoadBalancer(map[string]any{}))
}
