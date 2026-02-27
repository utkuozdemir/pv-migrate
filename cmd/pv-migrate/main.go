package main

import (
	"context"
	"log/slog"
	"os"

	// load all auth plugins - needed for gcp, azure etc.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/utkuozdemir/pv-migrate/internal/app"
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

	rootCmd, err := app.BuildMigrateCmd(ctx, version, commit, date)
	if err != nil {
		slog.Default().Error("❌ Failed to build command", "error", err.Error())

		return 1
	}

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		slog.Default().Error("❌ Failed to run", "error", err.Error())

		return 1
	}

	return 0
}
