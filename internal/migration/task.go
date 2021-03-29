package migration

import "github.com/utkuozdemir/pv-migrate/internal/k8s"

type Task struct {
	Id      string
	Source  *k8s.PvcInfo
	Dest    *k8s.PvcInfo
	Options RequestOptions
}
