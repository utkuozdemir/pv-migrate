package migration

const (
	DefaultRsyncImage    = "docker.io/instrumentisto/rsync-ssh:alpine"
	DefaultSshdImage     = "docker.io/panubo/sshd:1.3.0"
	DefaultIgnoreMounted = false
	DefaultNoChown       = false
)

type PVC struct {
	KubeconfigPath string
	Context        string
	Namespace      string
	Name           string
}

type Migration struct {
	Source     *PVC
	Dest       *PVC
	Options    *Options
	Strategies []string
	RsyncImage string
	SshdImage  string
}

type Options struct {
	DeleteExtraneousFiles bool
	IgnoreMounted         bool
	NoChown               bool
}
