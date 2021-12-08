package migration

import (
	log "github.com/sirupsen/logrus"
)

const (
	DefaultIgnoreMounted       = false
	DefaultNoChown             = false
	DefaultNoProgressBar       = false
	DefaultSourceMountReadOnly = true
)

type PVC struct {
	KubeconfigPath string
	Context        string
	Namespace      string
	Name           string
	Path           string
}

type Migration struct {
	Source     *PVC
	Dest       *PVC
	Options    *Options
	Strategies []string
	Logger     *log.Entry
}

type Options struct {
	DeleteExtraneousFiles bool
	IgnoreMounted         bool
	NoChown               bool
	NoProgressBar         bool
	SourceMountReadOnly   bool
	KeyAlgorithm          string
	HelmValuesFiles       []string
	HelmValues            []string
	HelmFileValues        []string
	HelmStringValues      []string
}
