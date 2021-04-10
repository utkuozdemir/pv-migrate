package k8s

import (
	"strings"
)

type Component string

const (
	// Rsync is the label value for the rsync client component of the application
	Rsync Component = "rsync"

	// Sshd is the label value for the sshd server component of the application
	Sshd Component = "sshd"

	appLabelKey         = "app.kubernetes.io/name"
	managedByLabelKey   = "app.kubernetes.io/managed-by"
	instanceLabelKey    = "app.kubernetes.io/instance"
	componentLabelKey   = "app.kubernetes.io/component"
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

func ComponentLabels(id string, component Component) map[string]string {
	labels := Labels(id)
	labels[componentLabelKey] = string(component)
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
