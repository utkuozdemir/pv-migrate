package strategy_test

import (
	"bytes"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/utkuozdemir/pv-migrate/internal/helm"
	"github.com/utkuozdemir/pv-migrate/internal/k8s"
	"github.com/utkuozdemir/pv-migrate/internal/migration"
	"github.com/utkuozdemir/pv-migrate/internal/pvc"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
)

func TestPeekAfterWaitsForInFlightPeek(t *testing.T) {
	t.Parallel()

	var (
		peekFinished atomic.Bool
		sharedBuf    bytes.Buffer
	)

	peekEntered := make(chan struct{})
	releasePeek := make(chan struct{})
	stopStarted := make(chan struct{})
	stopDone := make(chan struct{})

	stop := strategy.PeekAfter(0, func() {
		close(peekEntered)
		<-releasePeek
		sharedBuf.WriteString("peek\n")
		peekFinished.Store(true)
	})

	<-peekEntered

	go func() {
		close(stopStarted)
		stop()
		sharedBuf.WriteString("after-stop\n")
		close(stopDone)
	}()

	<-stopStarted

	select {
	case <-stopDone:
		t.Fatal("stop returned before in-flight peek finished")
	default:
	}

	close(releasePeek)
	<-stopDone

	assert.True(t, peekFinished.Load())
	assert.Equal(t, "peek\nafter-stop\n", sharedBuf.String())

	// Stopping a second time must be a safe no-op.
	stop()

	var ran atomic.Bool

	stopUnfired := strategy.PeekAfter(time.Hour, func() {
		ran.Store(true)
	})
	stopUnfired()
	assert.False(t, ran.Load())
}

func TestWriteMidInstallDiagnosticsIncludesAllReleaseNamespaces(t *testing.T) {
	t.Parallel()

	const releaseName = "pv-migrate-op-clusterip"

	srcInfo := unschedulablePodPVCInfo("src-ns", releaseName, "sshd-0", "0/3 nodes match affinity")
	dstInfo := unschedulablePodPVCInfo("dst-ns", releaseName, "rsync-0", "waiting for volume binding")

	t.Run("text", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		attempt := &migration.Attempt{
			Migration: &migration.Migration{
				Request:    &migration.Request{Writer: &out},
				SourceInfo: srcInfo,
				DestInfo:   dstInfo,
			},
			DiagnosticTargets: []migration.DiagnosticTarget{
				{Release: releaseName, Info: srcInfo},
				{Release: releaseName, Info: dstInfo},
			},
		}

		strategy.WriteMidInstallDiagnostics(t.Context(), attempt, releaseName,
			slog.New(slog.DiscardHandler))

		got := out.String()
		assert.Contains(t, got, "src-ns")
		assert.Contains(t, got, "0/3 nodes match affinity")
		assert.Contains(t, got, "dst-ns")
		assert.Contains(t, got, "waiting for volume binding")
	})

	t.Run("structured", func(t *testing.T) {
		t.Parallel()

		var logBuf bytes.Buffer

		attempt := &migration.Attempt{
			Migration: &migration.Migration{
				Request:    &migration.Request{Writer: io.Discard, StructuredLogs: true},
				SourceInfo: srcInfo,
				DestInfo:   dstInfo,
			},
			DiagnosticTargets: []migration.DiagnosticTarget{
				{Release: releaseName, Info: srcInfo},
				{Release: releaseName, Info: dstInfo},
			},
		}

		logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
		strategy.WriteMidInstallDiagnostics(t.Context(), attempt, releaseName, logger)

		got := logBuf.String()
		assert.Contains(t, got, `"namespace":"src-ns"`)
		assert.Contains(t, got, "0/3 nodes match affinity")
		assert.Contains(t, got, `"namespace":"dst-ns"`)
		assert.Contains(t, got, "waiting for volume binding")
	})
}

func TestClusterIPRecordsBothNamespacesBeforeInstallFails(t *testing.T) {
	t.Parallel()

	chart, err := helm.LoadChart("")
	require.NoError(t, err)

	restCfg := &rest.Config{Host: "https://127.0.0.1:1"}
	logger := slog.New(slog.DiscardHandler)
	cli := &k8s.ClusterClient{
		RestConfig:       restCfg,
		KubeClient:       fake.NewClientset(),
		RESTClientGetter: k8s.NewRESTClientGetter(restCfg, nil, logger),
	}

	srcInfo := &pvc.Info{
		ClusterClient: cli,
		Claim:         &corev1.PersistentVolumeClaim{Namespace: "src-ns", Name: "src"},
		SupportsROX:   true,
	}
	dstInfo := &pvc.Info{
		ClusterClient: cli,
		Claim:         &corev1.PersistentVolumeClaim{Namespace: "dst-ns", Name: "dst"},
		SupportsRWO:   true,
	}

	attempt := &migration.Attempt{
		HelmReleaseNamePrefix: "pv-migrate-op-clusterip",
		Migration: &migration.Migration{
			Chart: chart,
			Request: &migration.Request{
				KeyAlgorithm: "ed25519",
				HelmTimeout:  time.Second,
				Writer:       io.Discard,
			},
			SourceInfo: srcInfo,
			DestInfo:   dstInfo,
		},
	}

	err = (&strategy.ClusterIP{}).Run(t.Context(), attempt, logger)
	require.Error(t, err)
	require.Len(t, attempt.DiagnosticTargets, 2)
	assert.Equal(t, "src-ns", attempt.DiagnosticTargets[0].Info.Claim.Namespace)
	assert.Equal(t, "dst-ns", attempt.DiagnosticTargets[1].Info.Claim.Namespace)
}

func unschedulablePodPVCInfo(ns, releaseName, podName, message string) *pvc.Info {
	pod := &corev1.Pod{
		Namespace: ns,
		Name:      podName,
		Labels:    map[string]string{"app.kubernetes.io/instance": releaseName},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "app",
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{
						Reason:  "ErrImagePull",
						Message: message,
					},
				},
			}},
		},
	}

	return &pvc.Info{
		ClusterClient: &k8s.ClusterClient{
			RestConfig: &rest.Config{Host: "https://127.0.0.1:6443"},
			KubeClient: fake.NewClientset(pod),
		},
		Claim: &corev1.PersistentVolumeClaim{
			Namespace: ns, Name: "claim",
		},
	}
}
