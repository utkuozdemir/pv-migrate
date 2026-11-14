package pvc

// RequiredNodeAffinity returns Helm affinity values requiring scheduling on
// nodeName, or nil when nodeName is empty.
func RequiredNodeAffinity(nodeName string) map[string]any {
	return buildAffinityHelmValues(nodeName, true)
}
