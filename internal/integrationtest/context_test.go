package integrationtest

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
)

func prepareTestContext() *pvMigrateTestContext {
	if useKind() {
		return createKindTestContext()
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.WithError(err).Fatal("Couldn't get user home dir")
	}

	kubeconfig := fmt.Sprintf("%s/.kube/config", homeDir)
	client, config, err := buildKubeClient(filepath.Clean(kubeconfig))
	if err != nil {
		log.WithError(err).Fatal("Couldn't initialize kubernetes client")
	}

	return &pvMigrateTestContext{
		kubeClient: client,
		config:     config,
		kubeconfig: kubeconfig,
	}
}

func finalizeTestContext(c *pvMigrateTestContext) {
	if useKind() {
		err := destroyKindCluster(ctx.clusterProvider, c.kubeconfig)
		if err != nil {
			log.WithError(err).Fatal("failed to destroy kind cluster")
		}
	}
}
