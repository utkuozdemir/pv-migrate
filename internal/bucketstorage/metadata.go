package bucketstorage

import (
	"encoding/base64"
	"fmt"
	"time"

	"go.yaml.in/yaml/v4"
)

// Metadata holds information about a backup stored alongside the data in the bucket.
type Metadata struct {
	Version         int       `yaml:"version"`
	BackupTime      time.Time `yaml:"backupTime"`
	SourceNamespace string    `yaml:"sourceNamespace"`
	SourcePVC       string    `yaml:"sourcePvc"`
}

func generateMetadataBase64(namespace, pvcName string) (string, error) {
	meta := Metadata{
		Version:         1,
		BackupTime:      time.Now().UTC(),
		SourceNamespace: namespace,
		SourcePVC:       pvcName,
	}

	data, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("failed to marshal backup metadata: %w", err)
	}

	return base64.StdEncoding.EncodeToString(data), nil
}
