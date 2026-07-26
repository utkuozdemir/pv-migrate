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

	// DefaultS3Provider is rclone's generic S3-compatible provider mode.
	DefaultS3Provider = "Other"

	remoteName = "remote"
)

// ConfigOptions holds the high-level flags for generating an rclone.conf.
type ConfigOptions struct {
	Backend   string
	Provider  string
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string

	StorageAccount string
	StorageKey     string

	GCSServiceAccountJSON string
	GCSBucketPolicyOnly   *bool
}

// GenerateConfig produces an rclone.conf INI string from high-level options.
func GenerateConfig(opts ConfigOptions) (string, error) {
	if err := opts.validate(); err != nil {
		return "", err
	}

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

// validate rejects option values that cannot be represented in the generated
// config. An rclone.conf entry is one line, so a value carrying a newline would
// not be stored as that value: everything after the newline would be read as
// further config keys, silently producing a remote that differs from the one
// asked for. A credential piped in from a file or a Kubernetes secret is the
// most likely source. Carriage returns and NUL are rejected on the same grounds,
// and a leading or trailing space because rclone strips it on read.
//
// Only the fields the chosen backend actually writes are checked. The credential
// environment variables are all read regardless of backend, so a stray value left
// over from another backend would otherwise fail an operation whose config never
// contains it.
func (o ConfigOptions) validate() error {
	for _, field := range o.writtenFields() {
		if err := validateConfigValue(field.name, field.value); err != nil {
			return err
		}
	}

	return nil
}

// configField pairs a value with the name of the flag it came from, so an error
// points at what to change.
type configField struct {
	name  string
	value string
}

// writtenFields returns the option values that end up in the generated config for
// the chosen backend. The GCS backend contributes none: its credential is JSON,
// which is compacted onto one line and rejected by that compaction if malformed.
func (o ConfigOptions) writtenFields() []configField {
	switch o.Backend {
	case BackendS3:
		return []configField{
			{"s3-provider", o.Provider},
			{"endpoint", o.Endpoint},
			{"region", o.Region},
			{"access-key", o.AccessKey},
			{"secret-key", o.SecretKey},
		}
	case BackendAzure:
		return []configField{
			{"storage-account", o.StorageAccount},
			{"storage-key", o.StorageKey},
		}
	default:
		return nil
	}
}

func validateConfigValue(name, value string) error {
	if value == "" {
		return nil
	}

	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("--%s must not contain line breaks", name)
	}

	if strings.TrimSpace(value) != value {
		return fmt.Errorf("--%s must not have leading or trailing whitespace", name)
	}

	return nil
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

	provider := opts.Provider
	if provider == "" {
		provider = DefaultS3Provider
	}

	fmt.Fprintf(&builder, "provider = %s\n", provider)

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

	builder.WriteString("no_check_bucket = true\n")

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

	if opts.GCSBucketPolicyOnly == nil || *opts.GCSBucketPolicyOnly {
		builder.WriteString("bucket_policy_only = true\n")
	}

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
