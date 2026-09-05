package k8s_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/utkuozdemir/pv-migrate/internal/k8s"
)

func TestCanCreateNetworkPolicies(t *testing.T) {
	t.Parallel()

	cli := fake.NewClientset()

	cli.PrependReactor("create", "selfsubjectaccessreviews",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			createAction, ok := action.(k8stesting.CreateAction)
			require.True(t, ok)

			review, ok := createAction.GetObject().(*authorizationv1.SelfSubjectAccessReview)
			require.True(t, ok)

			attrs := review.Spec.ResourceAttributes
			assert.Equal(t, "create", attrs.Verb)
			assert.Equal(t, "networking.k8s.io", attrs.Group)
			assert.Equal(t, "networkpolicies", attrs.Resource)

			switch attrs.Namespace {
			case "yes":
				review.Status.Allowed = true
			case "no":
				review.Status.Allowed = false
			default:
				return true, nil, errors.New("authorizer unavailable")
			}

			return true, review, nil
		})

	allowed, err := k8s.CanCreateNetworkPolicies(t.Context(), cli, "yes")
	require.NoError(t, err)
	assert.True(t, allowed)

	allowed, err = k8s.CanCreateNetworkPolicies(t.Context(), cli, "no")
	require.NoError(t, err)
	assert.False(t, allowed)

	_, err = k8s.CanCreateNetworkPolicies(t.Context(), cli, "unknown")
	require.Error(t, err)
}
