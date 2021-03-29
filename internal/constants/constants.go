package constants

const (
	AppLabelKey         = "app"
	AppLabelValue       = "pv-migrate"
	InstanceLabelKey    = "instance"
	LabelSelectorPrefix = AppLabelKey + "=" + AppLabelValue + "," + InstanceLabelKey + "="
)
