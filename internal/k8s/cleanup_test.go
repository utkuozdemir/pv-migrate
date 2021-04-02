package k8s

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/utkuozdemir/pv-migrate/internal/constants"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestCleanupForIdServiceDeleted(t *testing.T) {
	id := "a1b2c"
	svc1 := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc1",
			Namespace: "namespace1",
			Labels: map[string]string{
				constants.AppLabelKey:      constants.AppLabelValue,
				constants.InstanceLabelKey: id,
			},
		},
	}

	kubeClient := fake.NewSimpleClientset(&svc1)
	err := CleanupForId(kubeClient, "namespace1", id)
	assert.Nil(t, err)

	services, _ := kubeClient.CoreV1().Services("namespace1").List(context.TODO(), metav1.ListOptions{})
	assert.Len(t, services.Items, 0)
}

func TestCleanupForIdOtherServicesNotDeleted(t *testing.T) {
	id := "a1b2c"
	svc1 := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc1",
			Namespace: "namespace1",
			Labels: map[string]string{
				constants.AppLabelKey:      constants.AppLabelValue,
				constants.InstanceLabelKey: "x1y1z",
			},
		},
	}

	svc2 := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc2",
			Namespace: "namespace1",
		},
	}

	svc3 := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc3",
			Namespace: "namespace3",
		},
	}


	kubeClient := fake.NewSimpleClientset(&svc1, &svc2, &svc3)
	err := CleanupForId(kubeClient, "namespace1", id)
	assert.Nil(t, err)

	services, _ := kubeClient.CoreV1().Services("").List(context.TODO(), metav1.ListOptions{})
	assert.Len(t, services.Items, 3)
}

// we cannot test much other than no error because the fake client we use does not support
// DeleteCollection operation: https://github.com/kubernetes/client-go/issues/609
func TestCleanupForIdNoError(t *testing.T) {
	id := "a1b2c"
	pod1 := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "namespace1",
			Labels: map[string]string{
				constants.AppLabelKey:      constants.AppLabelValue,
				constants.InstanceLabelKey: id,
			},
		},
	}

	job1 := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "job1",
			Namespace: "namespace1",
			Labels: map[string]string{
				constants.AppLabelKey:      constants.AppLabelValue,
				constants.InstanceLabelKey: id,
			},
		},
	}

	svc1 := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc1",
			Namespace: "namespace1",
			Labels: map[string]string{
				constants.AppLabelKey:      constants.AppLabelValue,
				constants.InstanceLabelKey: id,
			},
		},
	}

	kubeClient := fake.NewSimpleClientset(&pod1, &job1, &svc1)
	err := CleanupForId(kubeClient, "namespace1", id)
	assert.Nil(t, err)
}


