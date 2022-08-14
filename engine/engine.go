package engine

import (
	"context"

	"github.com/utkuozdemir/pv-migrate/internal/migrator"
	"github.com/utkuozdemir/pv-migrate/migration"
)

// Engine is the main component that coordinates and runs the migration.
// It is responsible for processing the request, building a migration task, determine the execution order
// of the strategies and execute them until one of them succeeds.
type Engine interface {
	// Run runs the migration
	Run(ctx context.Context, request *migration.Request) error
}

func New() Engine {
	return migrator.New()
}
