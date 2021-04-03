package main

import (
	"flag"
	log "github.com/sirupsen/logrus"
	"github.com/utkuozdemir/pv-migrate/internal/engine"
	"github.com/utkuozdemir/pv-migrate/internal/mountboth"
	"github.com/utkuozdemir/pv-migrate/internal/request"
	"github.com/utkuozdemir/pv-migrate/internal/rsyncsshcrosscluster"
	"github.com/utkuozdemir/pv-migrate/internal/rsyncsshincluster"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"math/rand"
	"os"
	"time"
	// needed for k8s oidc and gcp auth
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

var strategies = []strategy.Strategy{
	&mountboth.MountBoth{},
	&rsyncsshincluster.RsyncSshInCluster{},
	&rsyncsshcrosscluster.RsyncSshCrossCluster{},
}

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
	kubeconfig := flag.String("kubeconfig", "", "(optional) absolute path to the kubeconfig file")
	source := flag.String("source", "", "Source persistent volume claim")
	sourceNamespace := flag.String("source-namespace", "", "Source namespace")
	sourceContext := flag.String("source-context", "", "(optional) Source context")
	dest := flag.String("dest", "", "Destination persistent volume claim")
	destNamespace := flag.String("dest-namespace", "", "Destination namespace")
	destContext := flag.String("dest-context", "", "(optional) Destination context")
	deleteExtraneousFromDest := flag.Bool("dest-delete-extraneous-files", false, "(optional) delete extraneous files from destination dirs")
	flag.Parse()

	if *source == "" || *sourceNamespace == "" || *dest == "" || *destNamespace == "" {
		flag.Usage()
		return
	}

	sourceRequestPvc := request.NewPvc(*kubeconfig, *sourceContext, *sourceNamespace, *source)
	destRequestPvc := request.NewPvc(*kubeconfig, *destContext, *destNamespace, *dest)
	requestOptions := request.NewOptions(*deleteExtraneousFromDest)

	request := request.New(sourceRequestPvc, destRequestPvc, requestOptions, nil)
	logger := log.WithFields(request.LogFields())

	if *deleteExtraneousFromDest {
		logger.Warn("delete extraneous files from dest is enabled")
	}

	err := executeRequest(logger, request)
	if err != nil {
		logger.WithError(err).Fatal("Failed to initialize the engine")
		return
	}
}

func executeRequest(logger *log.Entry, request request.Request) error {
	engine, err := engine.New(strategies)
	if err != nil {
		logger.WithError(err).Error("Failed to initialize the engine")
		return err
	}

	numStrategies := len(strategies)
	strategyNames := strategy.Names(strategies)
	logger.WithField("strategies", strategyNames).Infof("Engine initialized with %v total strategies", numStrategies)

	err = engine.Run(request)
	if err != nil {
		logger.WithError(err).Error("Migration failed")
		return err
	}

	return nil
}
