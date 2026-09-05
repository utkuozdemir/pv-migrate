package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"unicode"
	"unicode/utf8"

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

	signalCh := make(chan os.Signal, 1)
	doneCh := make(chan struct{})

	defer close(doneCh)

	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	go func() {
		select {
		case <-signalCh:
			slog.Default().Warn("🔶 Received termination signal")

			cancel()
		case <-doneCh:
		}
	}()

	rootCmd, err := app.BuildMigrateCmd(ctx, version, commit, date, nil)
	if err != nil {
		slog.Default().Error("❌ Failed to build the command: " + err.Error())

		return 1
	}

	if err = rootCmd.ExecuteContext(ctx); err != nil {
		slog.Default().Error("❌ " + capitalized(err.Error()))

		return 1
	}

	return 0
}

// capitalized makes an error's message read as a sentence on the last line,
// since errors start in lowercase and the steps around them do not.
func capitalized(text string) string {
	first, size := utf8.DecodeRuneInString(text)
	if first == utf8.RuneError {
		return text
	}

	return string(unicode.ToUpper(first)) + text[size:]
}
