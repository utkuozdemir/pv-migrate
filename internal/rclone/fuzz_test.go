package rclone_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/internal/rclone"
)

// FuzzGenerateConfig checks the property the generated config has to hold for
// the migration job to do what was asked: every option that was accepted comes
// back out of the config as itself.
//
// rclone.conf has no escape syntax, so a value is only representable if it fits
// on one line. That makes "accepted" and "round-trips" the same question, and it
// is why the interesting failure is silent: an unrepresentable value does not
// produce an error, it produces a different remote. The config then lands in a
// Secret inside a pod, where nobody looks at it.
func FuzzGenerateConfig(f *testing.F) {
	f.Add("s3", "Other", "https://s3.example.com", "us-east-1", "ak", "sk", "", "")
	f.Add("azure", "", "", "", "", "", "account", "key")
	f.Add("gcs", "", "", "", "", "", "", "")
	// A newline in a value used to append further config keys. The endpoint here
	// is the one an injected `type` would replace.
	f.Add("s3", "Other", "https://s3.example.com", "us-east-1\ntype = local", "", "", "", "")
	f.Add("azure", "", "", "", "", "", "acct\nkey = other", "")
	f.Add("s3", "Other", "", "", "ak", "sk\nendpoint = http://elsewhere", "", "")

	f.Fuzz(func(t *testing.T,
		backend, provider, endpoint, region, accessKey, secretKey, storageAccount, storageKey string,
	) {
		opts := rclone.ConfigOptions{
			Backend:        backend,
			Provider:       provider,
			Endpoint:       endpoint,
			Region:         region,
			AccessKey:      accessKey,
			SecretKey:      secretKey,
			StorageAccount: storageAccount,
			StorageKey:     storageKey,
		}

		conf, err := rclone.GenerateConfig(opts)
		if err != nil {
			require.Empty(t, conf, "a rejected config must not be returned anyway")

			return
		}

		keys := parseRcloneConfig(t, conf)

		// Whatever the generator chose to write for a given option, it has to be
		// exactly the value it was given, not a prefix of it.
		for _, field := range []struct {
			key   string
			value string
		}{
			{"provider", provider},
			{"endpoint", endpoint},
			{"region", region},
			{"access_key_id", accessKey},
			{"secret_access_key", secretKey},
			{"account", storageAccount},
			{"key", storageKey},
		} {
			if got, ok := keys[field.key]; ok && field.value != "" {
				require.Equal(t, field.value, got, "key %q does not carry the value it was given", field.key)
			}
		}
	})
}

// parseRcloneConfig reads the generated config the way rclone does: a section
// header, then one key/value per line. It asserts the shape as it goes, because
// a value that broke out of its line shows up here as a duplicate key or as a
// second section rather than as a wrong value.
//
// This is deliberately a line reader rather than a general INI parser: the
// property is about the format this code emits, and a third-party parser would
// bring its own dialect, which is not the one rclone reads.
func parseRcloneConfig(t *testing.T, conf string) map[string]string {
	t.Helper()

	keys := make(map[string]string)
	sections := 0

	for line := range strings.SplitSeq(conf, "\n") {
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") {
			sections++

			require.Equal(t, "[remote]", line, "the generator owns the section name")

			continue
		}

		key, value, found := strings.Cut(line, " = ")
		require.True(t, found, "line %q is neither a section header nor a key/value pair", line)
		require.NotContains(t, keys, key, "duplicate key %q: a value escaped its line", key)

		keys[key] = value
	}

	require.Equal(t, 1, sections, "exactly one remote is generated")
	require.Contains(t, keys, "type", "every remote needs a type")

	return keys
}
