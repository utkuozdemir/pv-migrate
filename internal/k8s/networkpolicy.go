package k8s

import (
	"context"
	"fmt"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CanCreateNetworkPolicies reports whether the current account may create
// NetworkPolicy objects in the namespace, as the API server's own authorizer
// answers it.
func CanCreateNetworkPolicies(ctx context.Context, cli kubernetes.Interface, namespace string) (bool, error) {
	review := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      "create",
				Group:     "networking.k8s.io",
				Resource:  "networkpolicies",
			},
		},
	}

	result, err := cli.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to check the permission to create network policies in namespace %s: %w",
			namespace, err)
	}

	return result.Status.Allowed, nil
}
