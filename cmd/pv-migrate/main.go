package main

import (
	"math/rand"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/app"
	applog "github.com/utkuozdemir/pv-migrate/internal/log"
	// load all auth plugins - needed for gcp, azure etc.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	// will be overridden by goreleaser: https://goreleaser.com/cookbooks/using-main.version
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	logger, err := applog.New()
	if err != nil {
		log.Fatalf("Error: %s", err.Error())
	}

	rootCmd := app.New(logger, version, commit, date)

	err = rootCmd.Execute()
	if err != nil {
		logger.Fatalf(":cross_mark: Error: %s", err.Error())
	}
}
