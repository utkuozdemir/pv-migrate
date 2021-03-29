package migration

type Strategy interface {
	// Unique name of the strategy.
	//
	// Must follow kebab-case and return the same string every time.
	Name() string

	// Priority of the strategy.
	//
	// If the strategy is more preferable compared to another, it must return a smaller number (higher priority).
	//
	// Must always return the same number.
	Priority() int

	// True if this strategy can execute the task.
	//
	// Needs to evaluate the input and return if it can execute the task or not.
	CanDo(task *Task) bool

	// Execute the migration for the given task.
	//
	// Actual implementation of the migration.
	Run(task *Task) error

	// Clean up the created resources.
	//
	// The engine will call cleanup after the execution of the migration, no matter if it succeeds or fails.
	// It is recommended to implement this as best-effort, meaning that if it fails to remove one resource,
	// it shouldn't immediately return but proceed with the cleanup and
	// return all of the encountered errors combined at the end.
	Cleanup(task *Task) error
}
