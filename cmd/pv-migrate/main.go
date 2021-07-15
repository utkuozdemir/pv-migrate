package main

import (
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/app"
	"math/rand"
	"os"
	"time"
	// needed for k8s oidc and gcp auth
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

var (
	// will be overridden by goreleaser: https://goreleaser.com/environment/#using-the-mainversion
	version = "dev"
	commit  = "none"
)

func init() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
		PadLevelText:  true,
	})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
	rand.Seed(time.Now().UnixNano())
}

func main() {
	rootLogger := log.New()
	cliApp := app.New(rootLogger, version, commit)
	err := cliApp.Run(os.Args)
	if err != nil {
		rootLogger.Fatalf(":cross_mark: Error: %s", err.Error())
	}
}
