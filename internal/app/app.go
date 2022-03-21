package app

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	appName = "pv-migrate"
)

var logger *log.Entry

func New(rootLogger *log.Entry, version string, commit string, date string) *cobra.Command {
	logger = rootLogger

	return buildRootCmd(version, commit, date)
}
