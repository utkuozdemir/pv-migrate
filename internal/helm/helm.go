package helm

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"

	"helm.sh/helm/v4/pkg/chart/loader/archive"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
)

// chartFS is the embedded Helm chart.
//
// Note: The prefix "all:" is important here, as otherwise the files starting with "." or "_" will be ignored.
//
// See: https://github.com/golang/go/issues/44393
//
//go:embed all:pv-migrate
var chartFS embed.FS

const rootDir = "pv-migrate"

// LoadChart loads the embedded Helm chart.
func LoadChart() (*chart.Chart, error) {
	files, err := chartAsBufferedFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to get chart files: %w", err)
	}

	helmChart, err := loader.LoadFiles(files)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	return helmChart, nil
}

func chartAsBufferedFiles() ([]*archive.BufferedFile, error) {
	var files []*archive.BufferedFile

	err := fs.WalkDir(chartFS, rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		data, err := chartFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %q in chart: %w", path, err)
		}

		relativePath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return fmt.Errorf("failed to relativize path %q: %w", path, err)
		}

		// fix for Windows - the Helm client library expects templates to be under "templates/", i.e., with forward-slash
		relativePath = filepath.ToSlash(relativePath)

		files = append(files, &archive.BufferedFile{
			Name: relativePath,
			Data: data,
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk chart directory: %w", err)
	}

	return files, nil
}
