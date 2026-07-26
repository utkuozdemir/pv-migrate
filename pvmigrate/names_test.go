package pvmigrate_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/v2/util"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/utkuozdemir/pv-migrate/internal/opid"
	"github.com/utkuozdemir/pv-migrate/internal/strategy"
	"github.com/utkuozdemir/pv-migrate/pvmigrate"
)

// The operation ID is the only part of a Kubernetes resource name here that a
// user chooses, and everything around it is fixed. These are the fixed parts, so
// the test can compose the longest name the code can produce. The prefix comes
// from the code rather than being spelled out again, so a change to it cannot
// leave this test validating names nothing produces.

// sideSuffixes are appended to the release name prefix by the strategies that
// install one release per cluster side, plus the empty case for the ones that
// install a single release.
var sideSuffixes = []string{"", "-src", "-dest"}

// migrationComponents are the chart's rsync and sshd resources, which is what a
// migration release can contain. The empty case is the release name itself, which
// is also a label value.
var migrationComponents = []string{"", "-rsync", "-sshd"}

// operationComponents are the chart's rclone resources. Only a backup or restore
// release contains them, and such a release has no per-side suffix, which is why
// this family is enumerated separately rather than crossed with the strategies.
var operationComponents = []string{"", "-rclone"}

// operationMiddles are what the backup and restore commands use in the position
// where a migration uses a strategy name.
var operationMiddles = []string{"backup", "restore"}

// TestDerivedNamesFitTheirLimits is the reason the ID length limit is what it is.
// The ID is embedded in the name of the Helm release and, through it, in every
// resource the chart creates, and those two have different limits: Helm refuses a
// release name over 53 characters, and Kubernetes refuses a Service name or label
// value over 63. Helm's is the binding one, and going over it does not produce a
// clear error either. The install fails partway with a message about a generated
// name that the user never typed.
//
// The oracles here are Helm's own validator and apimachinery's, rather than the
// arithmetic being restated, and the combinations are enumerated rather than
// sampled. The strategy names come from the code, so adding one with a name longer
// than "loadbalancer" fails here instead of in a user's cluster. The suffixes below
// are spelled out, so a new or longer one has to be added to them as well.
func TestDerivedNamesFitTheirLimits(t *testing.T) {
	t.Parallel()

	longestID := strings.Repeat("a", pvmigrate.MaxIDLength)
	require.NoError(t, pvmigrate.ValidateID(longestID), "the longest allowed ID must itself be valid")

	migrationMiddles := make([]string, 0, len(pvmigrate.AllStrategies))
	for _, name := range pvmigrate.AllStrategies {
		migrationMiddles = append(migrationMiddles, string(name))
	}

	// The enumeration below is only complete if the advertised strategies are the
	// ones that actually run, since a strategy's name is what goes into the release
	// name. The two lists are separate, so this is what stops one drifting from the
	// other and taking the coverage above with it.
	_, err := strategy.GetStrategiesMapForNames(migrationMiddles)
	require.NoError(t, err, "an advertised strategy has no implementation")

	worstRelease, worstResource := "", ""

	for _, family := range []struct {
		middles    []string
		sides      []string
		components []string
	}{
		{middles: migrationMiddles, sides: sideSuffixes, components: migrationComponents},
		{middles: operationMiddles, sides: []string{""}, components: operationComponents},
	} {
		for _, middle := range family.middles {
			for _, side := range family.sides {
				release := opid.ReleasePrefix + longestID + "-" + middle + side

				require.NoError(t, util.ValidateReleaseName(release),
					"release name %q is %d chars and Helm will refuse it", release, len(release))

				if len(release) > len(worstRelease) {
					worstRelease = release
				}

				for _, component := range family.components {
					resource := release + component

					assert.Empty(t, validation.IsDNS1123Label(resource),
						"derived name %q (%d chars) is not a usable Kubernetes name", resource, len(resource))

					if len(resource) > len(worstResource) {
						worstResource = resource
					}
				}
			}
		}
	}

	t.Logf("longest release name is %d of the 53 Helm allows: %s", len(worstRelease), worstRelease)
	t.Logf("longest resource name is %d of the 63 Kubernetes allows: %s", len(worstResource), worstResource)
}

// TestGeneratedIDFitsLimit covers the IDs users do not choose. When --id is not
// given, a petname is generated and never validated, because it is trusted to be
// short. That trust rests on an external word list: if a longer adjective or name
// is ever added upstream, every migration without an explicit --id would start
// failing at Helm install time, and nothing else would catch it.
//
// The word list is not reachable from here, so this samples rather than enumerates.
// The draws are enough that the longest words in the list turn up.
func TestGeneratedIDFitsLimit(t *testing.T) {
	t.Parallel()

	longest := ""

	for range 200_000 {
		id := pvmigrate.GenerateID()
		if len(id) > len(longest) {
			longest = id
		}

		require.NoError(t, pvmigrate.ValidateID(id), "generated ID %q is not usable", id)
	}

	t.Logf("longest generated ID seen is %d of the %d allowed characters: %s",
		len(longest), pvmigrate.MaxIDLength, longest)
}
