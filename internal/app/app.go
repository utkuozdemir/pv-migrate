package app

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"github.com/utkuozdemir/pv-migrate/engine"
	applog "github.com/utkuozdemir/pv-migrate/internal/log"
	"github.com/utkuozdemir/pv-migrate/internal/rsync"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/migration"
	"os"
	"strings"
)

type cliAppContextKey string

const (
	authorName                    = "Utku Ozdemir"
	authorEmail                   = "uoz@protonmail.com"
	CommandMigrate                = "migrate"
	FlagLogLevel                  = "log-level"
	FlagLogFormat                 = "log-format"
	FlagSourceKubeconfig          = "source-kubeconfig"
	FlagSourceContext             = "source-context"
	FlagSourceNamespace           = "source-namespace"
	FlagSourcePath                = "source-path"
	FlagDestKubeconfig            = "dest-kubeconfig"
	FlagDestContext               = "dest-context"
	FlagDestNamespace             = "dest-namespace"
	FlagDestPath                  = "dest-path"
	FlagDestDeleteExtraneousFiles = "dest-delete-extraneous-files"
	FlagIgnoreMounted             = "ignore-mounted"
	FlagNoChown                   = "no-chown"
	FlagNoProgressBar             = "no-progress-bar"
	FlagSourceMountReadOnly       = "source-mount-read-only"
	FlagStrategies                = "strategies"
	FlagRsyncImage                = "rsync-image"
	FlagRsyncServiceAccount       = "rsync-service-account"
	FlagSshdImage                 = "sshd-image"
	FlagSshdServiceAccount        = "sshd-service-account"
	FlagSSHKeyAlgorithm           = "ssh-key-algorithm"

	loggerContextKey cliAppContextKey = "logger"
)

func New(rootLogger *log.Logger, version string, commit string) *cli.App {
	sshKeyAlgs := strings.Join(rsync.SSHKeyAlgorithms, ",")
	return &cli.App{
		Name:    "pv-migrate",
		Usage:   "A command-line utility to migrate data from one Kubernetes PersistentVolumeClaim to another",
		Version: fmt.Sprintf("%s (commit: %s)", version, commit),
		Commands: []*cli.Command{
			{
				Name:      CommandMigrate,
				Usage:     "Migrate data from the source PVC to the destination PVC",
				Aliases:   []string{"m"},
				ArgsUsage: "[SOURCE_PVC] [DESTINATION_PVC]",
				Action: func(c *cli.Context) error {
					logger := extractLogger(c.Context)

					s := migration.PVC{
						KubeconfigPath: c.String(FlagSourceKubeconfig),
						Context:        c.String(FlagSourceContext),
						Namespace:      c.String(FlagSourceNamespace),
						Name:           c.Args().Get(0),
						Path:           c.String(FlagSourcePath),
					}

					d := migration.PVC{
						KubeconfigPath: c.String(FlagDestKubeconfig),
						Context:        c.String(FlagDestContext),
						Namespace:      c.String(FlagDestNamespace),
						Name:           c.Args().Get(1),
						Path:           c.String(FlagDestPath),
					}

					opts := migration.Options{
						DeleteExtraneousFiles: c.Bool(FlagDestDeleteExtraneousFiles),
						IgnoreMounted:         c.Bool(FlagIgnoreMounted),
						SourceMountReadOnly:   c.Bool(FlagSourceMountReadOnly),
						NoChown:               c.Bool(FlagNoChown),
						NoProgressBar:         c.Bool(FlagNoProgressBar),
						KeyAlgorithm:          c.String(FlagSSHKeyAlgorithm),
					}

					strategies := strings.Split(c.String(FlagStrategies), ",")
					m := migration.Migration{
						Source:              &s,
						Dest:                &d,
						Options:             &opts,
						Strategies:          strategies,
						RsyncImage:          c.String(FlagRsyncImage),
						RsyncServiceAccount: c.String(FlagRsyncServiceAccount),
						SshdImage:           c.String(FlagSshdImage),
						SshdServiceAccount:  c.String(FlagSshdServiceAccount),
						Logger:              logger,
					}

					logger.Info(":rocket: Starting migration")
					if opts.DeleteExtraneousFiles {
						logger.Info(":white_exclamation_mark: " +
							"Extraneous files will be deleted from the destination")
					}

					return engine.New().Run(&m)
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        FlagSourceKubeconfig,
						Aliases:     []string{"k"},
						Usage:       "Path of the kubeconfig file of the source PVC",
						Value:       "",
						DefaultText: "~/.kube/config or KUBECONFIG env variable",
						TakesFile:   true,
					},
					&cli.StringFlag{
						Name:        FlagSourceContext,
						Aliases:     []string{"c"},
						Value:       "",
						Usage:       "Context in the kubeconfig file of the source PVC",
						DefaultText: "currently selected context in the source kubeconfig",
					},
					&cli.StringFlag{
						Name:        FlagSourceNamespace,
						Aliases:     []string{"n"},
						Usage:       "Namespace of the source PVC",
						Value:       "",
						DefaultText: "currently selected namespace in the source context",
					},
					&cli.StringFlag{
						Name:    FlagSourcePath,
						Aliases: []string{"p"},
						Usage:   "The filesystem path to migrate in the the source PVC",
						Value:   "/",
					},
					&cli.BoolFlag{
						Name:    FlagSourceMountReadOnly,
						Aliases: []string{"R"},
						Usage:   "Mount the source PVC in ReadOnly mode",
						Value:   migration.DefaultSourceMountReadOnly,
					},
					&cli.StringFlag{
						Name:        FlagDestKubeconfig,
						Aliases:     []string{"K"},
						Value:       "",
						Usage:       "Path of the kubeconfig file of the destination PVC",
						DefaultText: "~/.kube/config or KUBECONFIG env variable",
						TakesFile:   true,
					},
					&cli.StringFlag{
						Name:        FlagDestContext,
						Aliases:     []string{"C"},
						Value:       "",
						Usage:       "Context in the kubeconfig file of the destination PVC",
						DefaultText: "currently selected context in the destination kubeconfig",
					},
					&cli.StringFlag{
						Name:        FlagDestNamespace,
						Aliases:     []string{"N"},
						Usage:       "Namespace of the destination PVC",
						Value:       "",
						DefaultText: "currently selected namespace in the destination context",
					},
					&cli.StringFlag{
						Name:    FlagDestPath,
						Aliases: []string{"P"},
						Usage:   "The filesystem path to migrate in the the destination PVC",
						Value:   "/",
					},
					&cli.BoolFlag{
						Name:    FlagDestDeleteExtraneousFiles,
						Aliases: []string{"d"},
						Usage:   "Delete extraneous files on the destination by using rsync's '--delete' flag",
						Value:   false,
					},
					&cli.BoolFlag{
						Name:    FlagIgnoreMounted,
						Aliases: []string{"i"},
						Usage:   "Do not fail if the source or destination PVC is mounted",
						Value:   migration.DefaultIgnoreMounted,
					},
					&cli.BoolFlag{
						Name:    FlagNoChown,
						Aliases: []string{"o"},
						Usage:   "Omit chown on rsync",
						Value:   migration.DefaultNoChown,
					},
					&cli.BoolFlag{
						Name:    FlagNoProgressBar,
						Aliases: []string{"b"},
						Usage:   "Do not display a progress bar",
						Value:   migration.DefaultNoProgressBar,
					},
					&cli.StringFlag{
						Name:    FlagStrategies,
						Aliases: []string{"s"},
						Usage:   "The comma-separated list of strategies to be used in the given order",
						Value:   strings.Join(strategy.DefaultStrategies, ","),
					},
					&cli.StringFlag{
						Name:    FlagRsyncImage,
						Aliases: []string{"r"},
						Usage:   "Image to use for running rsync",
						Value:   migration.DefaultRsyncImage,
					},
					&cli.StringFlag{
						Name:  FlagRsyncServiceAccount,
						Usage: "Service account for the rsync pod",
						Value: migration.DefaultRsyncServiceAccount,
					},
					&cli.StringFlag{
						Name:    FlagSshdImage,
						Aliases: []string{"S"},
						Usage:   "Image to use for running sshd server",
						Value:   migration.DefaultSshdImage,
					},
					&cli.StringFlag{
						Name:  FlagSshdServiceAccount,
						Usage: "Service account for the sshd pod",
						Value: migration.DefaultSshdServiceAccount,
					},
					&cli.StringFlag{
						Name:    FlagSSHKeyAlgorithm,
						Aliases: []string{"a"},
						Usage:   fmt.Sprintf("SSH key algorithm to be used. Valid values are %s", sshKeyAlgs),
						Value:   rsync.Ed25519KeyAlgorithm,
					},
				},
			},
		},
		Flags: []cli.Flag{
			cli.HelpFlag,
			cli.VersionFlag,
			&cli.StringFlag{
				Name:    FlagLogLevel,
				Aliases: []string{"l"},
				Usage: fmt.Sprintf("Log level. Must be one of: %s",
					strings.Join(applog.LogLevels, ", ")),
				Value: "info",
			},
			&cli.StringFlag{
				Name:    FlagLogFormat,
				Aliases: []string{"f"},
				Usage: fmt.Sprintf("Log format. Must be one of: %s",
					strings.Join(applog.LogFormats, ", ")),
				Value: applog.LogFormatFancy,
			},
		},
		Before: func(c *cli.Context) error {
			l := c.String(FlagLogLevel)
			f := c.String(FlagLogFormat)
			entry, err := applog.BuildLogger(rootLogger, l, f)
			if err != nil {
				return err
			}

			ctx := c.Context
			c.Context = context.WithValue(ctx, loggerContextKey, entry)
			return nil
		},
		Authors: []*cli.Author{
			{
				Name:  authorName,
				Email: authorEmail,
			},
		},
		CommandNotFound: func(c *cli.Context, s string) {
			logger := extractLogger(c.Context)
			logger.Errorf(":cross_mark: Error: no help topic for '%s'", s)
			os.Exit(3)
		},
	}
}

func extractLogger(c context.Context) *log.Entry {
	return c.Value(loggerContextKey).(*log.Entry)
}
