package bucketstorage

type HelmActionConfigFunc = helmActionConfigFunc

var (
	BuildRemotePath      = buildRemotePath
	BuildHelmValues      = buildHelmValues
	MergeHelmValues      = mergeHelmValues
	PrintDetachMessage   = printDetachMessage
	RunRelease           = runRelease
	ShouldUploadMetadata = shouldUploadMetadata
	ValidateSubpath      = validateSubpath
)
