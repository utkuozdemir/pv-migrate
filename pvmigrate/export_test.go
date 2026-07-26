package pvmigrate

import "github.com/utkuozdemir/pv-migrate/internal/opid"

// The operation ID rules live in internal/opid, which is where both the generator
// and the validator are, so the two cannot disagree. They are re-exported here
// because the tests that cover them belong to this package's public API, which is
// where a caller meets those rules.
var (
	ValidateID = opid.Validate
	GenerateID = opid.Generate
)
