package migration

import (
	log "github.com/sirupsen/logrus"
)

const (
	DefaultRsyncImage          = "docker.io/utkuozdemir/pv-migrate-rsync:1.0.0"
	DefaultRsyncServiceAccount = "default"
	DefaultSshdImage           = "docker.io/utkuozdemir/pv-migrate-sshd:1.0.0"
	DefaultSshdServiceAccount  = "default"
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
	Source              *PVC
	Dest                *PVC
	Options             *Options
	Strategies          []string
	RsyncImage          string
	RsyncServiceAccount string
	SshdImage           string
	SshdServiceAccount  string
	Logger              *log.Entry
}

type Options struct {
	DeleteExtraneousFiles bool
	IgnoreMounted         bool
	NoChown               bool
	NoProgressBar         bool
	SourceMountReadOnly   bool
	KeyAlgorithm          string
}
