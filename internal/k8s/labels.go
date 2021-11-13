package k8s

import (
	"strings"
)

type Component string

const (
	appLabelKey         = "app.kubernetes.io/name"
	managedByLabelKey   = "app.kubernetes.io/managed-by"
	instanceLabelKey    = "app.kubernetes.io/instance"

	appLabelValue       = "pv-migrate"
	managedByLabelValue = "pv-migrate"
)

func Labels(id string) map[string]string {
	labels := map[string]string{
		appLabelKey:       appLabelValue,
		instanceLabelKey:  appLabelValue + "-" + id,
		managedByLabelKey: managedByLabelValue,
	}
	return labels
}


func LabelSelector(id string) string {
	labels := Labels(id)

	var elements []string
	for key, value := range labels {
		elements = append(elements, key+"="+value)
	}

	return strings.Join(elements, ",")
}
