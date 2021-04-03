package common

const (
	// AppLabelKey is the key of the label for the Kubernetes resources created by pv-migrate
	AppLabelKey = "app"
	// AppLabelValue is the value of the label for the Kubernetes resources created by pv-migrate
	AppLabelValue = "pv-migrate"
	// InstanceLabelKey is the key of the label for the Kubernetes resources created by a specific run of pv-migrate
	InstanceLabelKey = "instance"
	// LabelSelectorFormat is the format to be used to build the label selector for a specific run of pv-migrate
	LabelSelectorFormat = AppLabelKey + "=" + AppLabelValue + "," + InstanceLabelKey + "=%s"
)
