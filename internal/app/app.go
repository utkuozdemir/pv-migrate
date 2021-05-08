package app

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"github.com/utkuozdemir/pv-migrate/engine"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/migration"
	"strings"
)

const (
	authorName                    = "Utku Ozdemir"
	authorEmail                   = "uoz@protonmail.com"
	CommandMigrate                = "migrate"
	FlagSourceKubeconfig          = "source-kubeconfig"
	FlagSourceContext             = "source-context"
	FlagSourceNamespace           = "source-namespace"
	FlagDestKubeconfig            = "dest-kubeconfig"
	FlagDestContext               = "dest-context"
	FlagDestNamespace             = "dest-namespace"
	FlagDestDeleteExtraneousFiles = "dest-delete-extraneous-files"
	FlagIgnoreMounted             = "ignore-mounted"
	FlagNoChown                   = "no-chown"
	FlagStrategies                = "strategies"
	FlagRsyncImage                = "rsync-image"
	FlagSshdImage                 = "sshd-image"
)

func New(version string, commit string) *cli.App {
	return &cli.App{
		Name:                 "pv-migrate",
		Usage:                "A command-line utility to migrate data from one Kubernetes PersistentVolumeClaim to another",
		Version:              fmt.Sprintf("%s (commit: %s)", version, commit),
		EnableBashCompletion: true,
		Commands: []*cli.Command{
			&completionCommand,
			{
				Name:      CommandMigrate,
				Usage:     "Migrate data from the source PVC to the destination PVC",
				Aliases:   []string{"m"},
				ArgsUsage: "[SOURCE_PVC] [DESTINATION_PVC]",
				Action: func(c *cli.Context) error {
					s := migration.PVC{
						KubeconfigPath: c.String(FlagSourceKubeconfig),
						Context:        c.String(FlagSourceContext),
						Namespace:      c.String(FlagSourceNamespace),
						Name:           c.Args().Get(0),
					}

					d := migration.PVC{
						KubeconfigPath: c.String(FlagDestKubeconfig),
						Context:        c.String(FlagDestContext),
						Namespace:      c.String(FlagDestNamespace),
						Name:           c.Args().Get(1),
					}

					opts := migration.Options{
						DeleteExtraneousFiles: c.Bool(FlagDestDeleteExtraneousFiles),
						IgnoreMounted:         c.Bool(FlagIgnoreMounted),
						NoChown:               c.Bool(FlagNoChown),
					}

					strategies := strings.Split(c.String(FlagStrategies), ",")
					m := migration.Migration{
						Source:     &s,
						Dest:       &d,
						Options:    &opts,
						Strategies: strategies,
						RsyncImage: c.String(FlagRsyncImage),
						SshdImage:  c.String(FlagSshdImage),
					}

					if opts.DeleteExtraneousFiles {
						log.Info("Extraneous files will be deleted from the destination")
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
						Name:    FlagSshdImage,
						Aliases: []string{"S"},
						Usage:   "Image to use for running sshd server",
						Value:   migration.DefaultSshdImage,
					},
				},
			},
		},
		Authors: []*cli.Author{
			{
				Name:  authorName,
				Email: authorEmail,
			},
		},
	}
}
