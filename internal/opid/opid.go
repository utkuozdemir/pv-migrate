// Package opid deals with the identifier that names one migration, backup or
// restore.
//
// The identifier is what `pv-migrate status` and `pv-migrate cleanup` are given
// to find an operation again, and it is embedded in the name of every Kubernetes
// resource the operation creates, which is what constrains its shape and length.
package opid

import (
	"errors"
	"fmt"
	"regexp"

	petname "github.com/dustinkirkland/golang-petname"
)

// ReleasePrefix is what every Helm release created for an operation is named
// after, followed by the identifier. It is also what `cleanup` and `status` match
// on to find an operation's releases again, which is why it lives next to the
// identifier rules rather than being spelled out at each of those places.
const ReleasePrefix = "pv-migrate-"

// MaxLength limits the identifier so that every name derived from it is
// acceptable to both Helm and Kubernetes.
//
// The binding limit is Helm's, not Kubernetes': Helm refuses a release name
// longer than 53 characters, while Kubernetes allows 63 for the Service names and
// label values the chart goes on to derive. The 29 reserved characters are the
// worst-case release name around the identifier, which is the "pv-migrate-"
// prefix, the longest strategy name and the cluster-side suffix.
//
// pvmigrate's derived-name test composes those names and hands them to Helm's own
// validator, so this arithmetic is checked rather than asserted.
const MaxLength = 53 - 29

// wordsPerName is how many petname words a generated identifier is made of. Two
// is enough to be memorable and to make a collision between two operations
// running at once unlikely, while staying well inside MaxLength.
const wordsPerName = 2

var valid = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Generate returns a fresh identifier, for when the user did not supply one. The
// result always satisfies Validate.
func Generate() string {
	return petname.Generate(wordsPerName, "-")
}

// Validate checks that id can be used in Kubernetes resource names: lowercase
// alphanumeric with single hyphens between segments, and short enough that the
// names derived from it still fit.
func Validate(id string) error {
	if len(id) == 0 {
		return errors.New("operation ID must not be empty")
	}

	if len(id) > MaxLength {
		return fmt.Errorf("operation ID %q is too long (%d chars), maximum is %d", id, len(id), MaxLength)
	}

	if !valid.MatchString(id) {
		return fmt.Errorf("operation ID %q is invalid: must be lowercase alphanumeric with optional hyphens, "+
			"and must not start or end with a hyphen", id)
	}

	return nil
}
