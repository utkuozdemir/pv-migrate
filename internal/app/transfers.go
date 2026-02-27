package app

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/utkuozdemir/pv-migrate/pvmigrate"
)

// transferEntry represents a single source→dest PVC pair in a transfers YAML file.
type transferEntry struct {
	Source          string `yaml:"source"`
	Dest            string `yaml:"dest"`
	SourceNamespace string `yaml:"sourceNamespace,omitempty"`
	DestNamespace   string `yaml:"destNamespace,omitempty"`
	SourcePath      string `yaml:"sourcePath,omitempty"`
	DestPath        string `yaml:"destPath,omitempty"`
}

// parseTransfersFile reads a YAML file containing a list of transfer pairs.
//
// Example YAML:
//
//   - source: pvc-a
//     dest: new-pvc-a
//   - source: pvc-b
//     dest: new-pvc-b
//     destNamespace: other-ns
func parseTransfersFile(filePath string) ([]pvmigrate.Transfer, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read transfers file: %w", err)
	}

	var entries []transferEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse transfers file: %w", err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("transfers file is empty")
	}

	transfers := make([]pvmigrate.Transfer, 0, len(entries))

	for i, entry := range entries {
		if entry.Source == "" {
			return nil, fmt.Errorf("transfer %d: source is required", i+1)
		}

		if entry.Dest == "" {
			return nil, fmt.Errorf("transfer %d: dest is required", i+1)
		}

		transfers = append(transfers, pvmigrate.Transfer{
			Source: pvmigrate.PVC{
				Name:      entry.Source,
				Namespace: entry.SourceNamespace,
				Path:      entry.SourcePath,
			},
			Dest: pvmigrate.PVC{
				Name:      entry.Dest,
				Namespace: entry.DestNamespace,
				Path:      entry.DestPath,
			},
		})
	}

	return transfers, nil
}
