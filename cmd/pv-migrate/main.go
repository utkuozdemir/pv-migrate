package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
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

	go watchSignal(signalCh, doneCh, signal.Stop, cancel)

	rootCmd, err := app.BuildMigrateCmd(ctx, version, commit, date, nil)
	if err != nil {
		slog.Default().Error("❌ Failed to build the command", "error", err)

		return 1
	}

	if err = rootCmd.ExecuteContext(ctx); err != nil {
		// "migration failed: <cause>" reads as the message and its error
		message, cause, found := strings.Cut(err.Error(), ": ")
		if !found || !strings.HasSuffix(message, " failed") {
			slog.Default().Error("❌ " + capitalized(err.Error()))

			return 1
		}

		slog.Default().Error("❌ "+capitalized(message), "error", cause)

		return 1
	}

	return 0
}

// watchSignal cancels ctx on the first termination signal and unregisters
// signalCh so a second signal terminates the process instead of being dropped.
func watchSignal(
	signalCh chan os.Signal,
	doneCh <-chan struct{},
	stop func(chan<- os.Signal),
	cancel context.CancelFunc,
) {
	select {
	case <-signalCh:
		stop(signalCh)
		slog.Default().Warn("🔶 Received termination signal")

		cancel()
	case <-doneCh:
	}
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
