package pvmigrate_test

import (
	"context"
	"log"
	"log/slog"

	"github.com/utkuozdemir/pv-migrate/pvmigrate"
)

//nolint:testableexamples // cannot validate output without a real cluster
func Example() {
	migration := pvmigrate.NewMigration()

	migration.Source = pvmigrate.PVC{
		Context:   "source-cluster",
		Namespace: "source-ns",
		Name:      "source-pvc",
	}

	migration.Dest = pvmigrate.PVC{
		KubeconfigPath: "/home/user/.kube/other-config",
		Namespace:      "dest-ns",
		Name:           "dest-pvc",
		Path:           "/some-sub-path/",
	}

	migration.Strategies = []pvmigrate.Strategy{pvmigrate.Mount, pvmigrate.ClusterIP}
	migration.KeyAlgorithm = pvmigrate.Ed25519
	migration.Compress = true
	migration.DeleteExtraneousFiles = true
	migration.Logger = slog.Default()

	if err := pvmigrate.Run(context.Background(), &migration); err != nil {
		log.Fatal(err)
	}
}
