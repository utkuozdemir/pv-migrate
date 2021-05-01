package app

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"github.com/utkuozdemir/pv-migrate/internal/engine"
	"github.com/utkuozdemir/pv-migrate/internal/request"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
)

const (
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
					sourceKubeconfig := c.String(FlagSourceKubeconfig)
					sourceContext := c.String(FlagSourceContext)
					sourceNamespace := c.String(FlagSourceNamespace)
					source := c.Args().Get(0)
					destKubeconfig := c.String(FlagDestKubeconfig)
					destContext := c.String(FlagDestContext)
					destNamespace := c.String(FlagDestNamespace)
					dest := c.Args().Get(1)
					destDeleteExtraneousFiles := c.Bool(FlagDestDeleteExtraneousFiles)
					ignoreMounted := c.Bool(FlagIgnoreMounted)
					noChown := c.Bool(FlagNoChown)
					strategies := c.StringSlice(FlagStrategies)
					sourceRequestPvc := request.NewPVC(sourceKubeconfig, sourceContext, sourceNamespace, source)
					destRequestPvc := request.NewPVC(destKubeconfig, destContext, destNamespace, dest)
					requestOptions := request.NewOptions(destDeleteExtraneousFiles, ignoreMounted, noChown)
					rsyncImage := c.String(FlagRsyncImage)
					sshdImage := c.String(FlagSshdImage)

					req := request.New(sourceRequestPvc, destRequestPvc, requestOptions,
						strategies, rsyncImage, sshdImage)

					if destDeleteExtraneousFiles {
						log.WithFields(req.LogFields()).Info("Extraneous files will be deleted from the destination")
					}

					return executeRequest(req)
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
						Value:   request.DefaultIgnoreMounted,
					},
					&cli.BoolFlag{
						Name:    FlagNoChown,
						Aliases: []string{"o"},
						Usage:   "Omit chown on rsync",
						Value:   request.DefaultNoChown,
					},
					&cli.StringSliceFlag{
						Name:    FlagStrategies,
						Aliases: []string{"s"},
						Usage:   "The strategies to be used in the given order",
						Value:   cli.NewStringSlice(strategy.Defaults...),
					},
					&cli.StringFlag{
						Name:    FlagRsyncImage,
						Aliases: []string{"r"},
						Usage:   "Image to use for running rsync",
						Value:   request.DefaultRsyncImage,
					},
					&cli.StringFlag{
						Name:    FlagSshdImage,
						Aliases: []string{"S"},
						Usage:   "Image to use for running sshd server",
						Value:   request.DefaultSshdImage,
					},
				},
			},
		},
		Authors: []*cli.Author{
			{
				Name:  "Utku Ozdemir",
				Email: "uoz@protonmail.com",
			},
		},
	}
}

func executeRequest(r request.Request) error {
	logger := log.WithFields(r.LogFields())
	eng := engine.New()
	err := eng.Run(r)
	if err != nil {
		logger.WithError(err).Error("Migration failed")
	}
	return err
}
