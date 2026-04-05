package rclone

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	BackendS3    = "s3"
	BackendAzure = "azure"
	BackendGCS   = "gcs"

	remoteName = "remote"
)

// ConfigOptions holds the high-level flags for generating an rclone.conf.
type ConfigOptions struct {
	Backend   string
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string

	StorageAccount string
	StorageKey     string

	GCSServiceAccountJSON string
}

// GenerateConfig produces an rclone.conf INI string from high-level options.
func GenerateConfig(opts ConfigOptions) (string, error) {
	switch opts.Backend {
	case BackendS3:
		return generateS3Config(opts)
	case BackendAzure:
		return generateAzureConfig(opts), nil
	case BackendGCS:
		return generateGCSConfig(opts)
	case "":
		return "", errors.New("backend must not be empty")
	default:
		return "", fmt.Errorf("unsupported backend: %s", opts.Backend)
	}
}

// ReadConfigFile reads a raw rclone.conf file from disk.
func ReadConfigFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read rclone config file %s: %w", path, err)
	}

	return string(data), nil
}

func generateS3Config(opts ConfigOptions) (string, error) {
	var builder strings.Builder

	fmt.Fprintf(&builder, "[%s]\n", remoteName)
	builder.WriteString("type = s3\n")
	builder.WriteString("provider = Other\n")

	if opts.Endpoint != "" {
		fmt.Fprintf(&builder, "endpoint = %s\n", opts.Endpoint)
	}

	if opts.Region != "" {
		fmt.Fprintf(&builder, "region = %s\n", opts.Region)
	}

	switch {
	case opts.AccessKey != "" && opts.SecretKey != "":
		fmt.Fprintf(&builder, "access_key_id = %s\n", opts.AccessKey)
		fmt.Fprintf(&builder, "secret_access_key = %s\n", opts.SecretKey)
	case opts.AccessKey != "" || opts.SecretKey != "":
		return "", errors.New("both access-key and secret-key must be provided together")
	default:
		builder.WriteString("env_auth = true\n")
	}

	return builder.String(), nil
}

func generateAzureConfig(opts ConfigOptions) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "[%s]\n", remoteName)
	builder.WriteString("type = azureblob\n")

	if opts.StorageAccount != "" {
		fmt.Fprintf(&builder, "account = %s\n", opts.StorageAccount)
	}

	if opts.StorageKey != "" {
		fmt.Fprintf(&builder, "key = %s\n", opts.StorageKey)
	} else {
		builder.WriteString("env_auth = true\n")
	}

	return builder.String()
}

func generateGCSConfig(opts ConfigOptions) (string, error) {
	var builder strings.Builder

	fmt.Fprintf(&builder, "[%s]\n", remoteName)
	builder.WriteString("type = google cloud storage\n")

	builder.WriteString("bucket_policy_only = true\n")

	if opts.GCSServiceAccountJSON != "" {
		compacted, err := compactJSON(opts.GCSServiceAccountJSON)
		if err != nil {
			return "", fmt.Errorf("failed to compact GCS service account JSON: %w", err)
		}

		fmt.Fprintf(&builder, "service_account_credentials = %s\n", compacted)
	} else {
		builder.WriteString("env_auth = true\n")
	}

	return builder.String(), nil
}

// compactJSON removes insignificant whitespace from a JSON string so it fits on
// a single INI config line.
func compactJSON(input string) (string, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(input)); err != nil {
		return "", err
	}

	return buf.String(), nil
}
