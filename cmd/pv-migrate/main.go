package main

import (
	"context"
	"fmt"
	"os"

	// load all auth plugins - needed for gcp, azure etc.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/utkuozdemir/pv-migrate/app"
	applog "github.com/utkuozdemir/pv-migrate/log"
)

var (
	// will be overridden by goreleaser: https://goreleaser.com/cookbooks/using-main.version/
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if exitCode := run(); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func run() int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger, err := applog.New(ctx)
	if err != nil {
		fmt.Printf("❌ Error: %s\n", err.Error()) //nolint:forbidigo

		return 1
	}

	rootCmd := app.New(logger, version, commit, date)

	err = rootCmd.ExecuteContext(ctx)
	if err != nil {
		logger.Errorf("❌ Error: %s", err.Error())

		return 1
	}

	return 0
}
