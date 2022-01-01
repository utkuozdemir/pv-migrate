package engine

import (
	"github.com/utkuozdemir/pv-migrate/internal/migrator"
	"github.com/utkuozdemir/pv-migrate/migration"
)

// Engine is the main component that coordinates and runs the migration.
// It is responsible of processing the request, building a migration task, determine the execution order
// of the strategies and execute them until one of them succeeds.
type Engine interface {
	// Run runs the migration
	Run(r *migration.Request) error
}

func New() Engine {
	return migrator.New()
}
