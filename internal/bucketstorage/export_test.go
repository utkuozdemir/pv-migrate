package bucketstorage

type HelmActionConfigFunc = helmActionConfigFunc

var (
	ApplyPosixMetadataValues = applyPosixMetadataValues
	BuildHelmValues          = buildHelmValues
	BuildRcloneCommand       = buildRcloneCommand
	BuildRemotePath          = buildRemotePath
	MergeHelmValues          = mergeHelmValues
	PrintDetachMessage       = printDetachMessage
	ResolveLocalPath         = resolveLocalPath
	RunRelease               = runRelease
	ShouldUploadMetadata     = shouldUploadMetadata
	ValidateSubpath          = validateSubpath
)
