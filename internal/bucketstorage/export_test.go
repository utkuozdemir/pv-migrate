package bucketstorage

type HelmActionConfigFunc = helmActionConfigFunc

var (
	BuildRemotePath      = buildRemotePath
	BuildHelmValues      = buildHelmValues
	MergeHelmValues      = mergeHelmValues
	RunRelease           = runRelease
	ShouldUploadMetadata = shouldUploadMetadata
	ValidateSubpath      = validateSubpath
)
