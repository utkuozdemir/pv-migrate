package app

const (
	EnvS3AccessKey           = envS3AccessKey
	EnvS3SecretKey           = envS3SecretKey
	EnvAzureStorageAccount   = envAzureStorageAccount
	EnvAzureStorageKey       = envAzureStorageKey
	EnvGCSServiceAccountJSON = envGCSServiceAccountJSON
)

var ApplyBucketStorageEnvDefaults = applyBucketStorageEnvDefaults

var (
	ReleaseImageTag     = releaseImageTag
	ReleaseChartVersion = releaseChartVersion
)
