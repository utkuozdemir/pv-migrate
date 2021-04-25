package app

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"github.com/utkuozdemir/pv-migrate/internal/engine"
	"github.com/utkuozdemir/pv-migrate/internal/mountboth"
	"github.com/utkuozdemir/pv-migrate/internal/request"
	"github.com/utkuozdemir/pv-migrate/internal/rsyncsshcrosscluster"
	"github.com/utkuozdemir/pv-migrate/internal/rsyncsshincluster"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"strings"
)

const (
	flagSourceKubeconfig          = "source-kubeconfig"
	flagSourceContext             = "source-context"
	flagSourceNamespace           = "source-namespace"
	flagDestKubeconfig            = "dest-kubeconfig"
	flagDestContext               = "dest-context"
	flagDestNamespace             = "dest-namespace"
	flagDestDeleteExtraneousFiles = "dest-delete-extraneous-files"
	flagOverrideStrategies        = "override-strategies"
	flagRsyncImage                = "rsync-image"
	flagSshdImage                 = "sshd-image"
)

var (
	strategies = []strategy.Strategy{
		&mountboth.MountBoth{},
		&rsyncsshincluster.RsyncSSSHInCluster{},
		&rsyncsshcrosscluster.RsyncSSHCrossCluster{},
	}
)

func New(version string, commit string) *cli.App {
	return &cli.App{
		Name:    "pv-migrate",
		Usage:   "A command-line utility to migrate data from one Kubernetes PersistentVolumeClaim to another",
		Version: fmt.Sprintf("%s (commit: %s)", version, commit),
		Commands: []*cli.Command{
			{
				Name:      "migrate",
				Usage:     "Migrate data from the source pvc to the destination pvc",
				Aliases:   []string{"m"},
				ArgsUsage: "[SOURCE_PVC] [DESTINATION_PVC]",
				Action: func(c *cli.Context) error {
					sourceKubeconfig := c.String(flagSourceKubeconfig)
					sourceContext := c.String(flagSourceContext)
					sourceNamespace := c.String(flagSourceNamespace)
					source := c.Args().Get(0)
					destKubeconfig := c.String(flagDestKubeconfig)
					destContext := c.String(flagDestContext)
					destNamespace := c.String(flagDestNamespace)
					dest := c.Args().Get(1)
					destDeleteExtraneousFiles := c.Bool(flagDestDeleteExtraneousFiles)
					overrideStrategies := c.StringSlice(flagOverrideStrategies)
					sourceRequestPvc := request.NewPVC(sourceKubeconfig, sourceContext, sourceNamespace, source)
					destRequestPvc := request.NewPVC(destKubeconfig, destContext, destNamespace, dest)
					requestOptions := request.NewOptions(destDeleteExtraneousFiles)
					rsyncImage := c.String(flagRsyncImage)
					sshdImage := c.String(flagSshdImage)

					req := request.New(sourceRequestPvc, destRequestPvc, requestOptions,
						overrideStrategies, rsyncImage, sshdImage)
					logger := log.WithFields(req.LogFields())

					if destDeleteExtraneousFiles {
						logger.Info("Extraneous files will be deleted from the destination")
					}

					return executeRequest(logger, req)
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        flagSourceKubeconfig,
						Aliases:     []string{"k"},
						Usage:       "Path of the kubeconfig file of the source pvc",
						Value:       "",
						DefaultText: "~/.kube/config or KUBECONFIG env variable",
						TakesFile:   true,
					},
					&cli.StringFlag{
						Name:        flagSourceContext,
						Aliases:     []string{"c"},
						Value:       "",
						Usage:       "Context in the kubeconfig file of the source pvc",
						DefaultText: "currently selected context in the source kubeconfig",
					},
					&cli.StringFlag{
						Name:        flagSourceNamespace,
						Aliases:     []string{"n"},
						Usage:       "Namespace of the source pvc",
						Value:       "",
						DefaultText: "currently selected namespace in the source context",
					},
					&cli.StringFlag{
						Name:        flagDestKubeconfig,
						Aliases:     []string{"K"},
						Value:       "",
						Usage:       "Path of the kubeconfig file of the destination pvc",
						DefaultText: "~/.kube/config or KUBECONFIG env variable",
						TakesFile:   true,
					},
					&cli.StringFlag{
						Name:        flagDestContext,
						Aliases:     []string{"C"},
						Value:       "",
						Usage:       "Context in the kubeconfig file of the destination pvc",
						DefaultText: "currently selected context in the destination kubeconfig",
					},
					&cli.StringFlag{
						Name:        flagDestNamespace,
						Aliases:     []string{"N"},
						Usage:       "Namespace of the destination pvc",
						Value:       "",
						DefaultText: "currently selected namespace in the destination context",
					},
					&cli.BoolFlag{
						Name:    flagDestDeleteExtraneousFiles,
						Aliases: []string{"d"},
						Usage:   "Delete extraneous files on the destination by using rsync's '--delete' flag",
						Value:   false,
					},
					&cli.StringSliceFlag{
						Name:        flagOverrideStrategies,
						Aliases:     []string{"s"},
						Usage:       "Override the default list of strategies and their order by priority",
						Value:       nil,
						DefaultText: "try all built-in strategies in the natural order",
					},
					&cli.StringFlag{
						Name:    flagRsyncImage,
						Aliases: []string{"r"},
						Usage:   "Image to use for running rsync",
						Value:   request.DefaultRsyncImage,
					},
					&cli.StringFlag{
						Name:    flagSshdImage,
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

func executeRequest(logger *log.Entry, request request.Request) error {
	eng, err := engine.New(strategies)
	if err != nil {
		logger.WithError(err).Error("Failed to initialize the engine")
		return err
	}

	numStrategies := len(strategies)
	strategyNames := strategy.Names(strategies)
	logger.WithField("strategies", strings.Join(strategyNames, " ")).
		Infof("Engine initialized with %v total strategies", numStrategies)

	err = eng.Run(request)
	if err != nil {
		logger.WithError(err).Error("Migration failed")
		return err
	}

	return nil
}
