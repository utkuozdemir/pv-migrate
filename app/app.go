package app

import (
	"context"

	"github.com/spf13/cobra"
)

const (
	appName = "pv-migrate"
)

func New(ctx context.Context, version string, commit string, date string) *cobra.Command {
	return buildRootCmd(ctx, version, commit, date)
}
