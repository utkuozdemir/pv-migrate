package strategy

import (
	"fmt"
	"github.com/utkuozdemir/pv-migrate/internal/job"
	"github.com/utkuozdemir/pv-migrate/internal/task"
)

var (
	Defaults = []string{Mnt2Name, SvcName, LbSvcName}

	all = []Strategy{
		&Mnt2{},
		&Svc{},
		&LbSvc{},
	}

	allMap = make(map[string]Strategy)
)

func init() {
	for _, s := range all {
		allMap[s.Name()] = s
	}
}

type Strategy interface {
	// Name is the unique name of the strategy.
	//
	// Must follow kebab-case and return the same string every time.
	Name() string

	// CanDo must return True if this strategy can execute the job.
	//
	// Needs to evaluate the input and return if it can execute the job or not.
	CanDo(task job.Job) bool

	// Run executes the migration for the given task.
	//
	// This is the actual implementation of the migration.
	Run(task task.Task) error

	// Cleanup is the function to remove the temporary resources used for the migration.
	//
	// The engine will call cleanup after the execution of the migration, no matter if it succeeds or fails.
	// It is recommended to implement this as best-effort, meaning that if it fails to remove one resource,
	// it shouldn't immediately return but proceed with the cleanup and
	// return all of the encountered errors combined at the end.
	Cleanup(task task.Task) error
}

// Names extracts a slice of names of the given strategies.
func Names(strategies []Strategy) []string {
	var result []string
	for _, strategy := range strategies {
		name := strategy.Name()
		result = append(result, name)
	}
	return result
}

func ForNames(names []string) ([]Strategy, error) {
	var sts []Strategy
	for _, name := range names {
		s, ok := allMap[name]
		if !ok {
			return nil, fmt.Errorf("strategy not found: %s", name)
		}

		sts = append(sts, s)
	}
	return sts, nil
}
