package pvmigrate_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utkuozdemir/pv-migrate/pvmigrate"
)

//nolint:funlen
func TestValidateID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		id         string
		wantErrMsg string
	}{
		{
			name: "simple lowercase",
			id:   "mydb",
		},
		{
			name: "with hyphens",
			id:   "my-migration-1",
		},
		{
			name: "single char",
			id:   "a",
		},
		{
			name: "numbers only",
			id:   "12345",
		},
		{
			name: "max length",
			id:   strings.Repeat("a", 28),
		},
		{
			name:       "empty",
			id:         "",
			wantErrMsg: "must not be empty",
		},
		{
			name:       "too long",
			id:         strings.Repeat("a", 29),
			wantErrMsg: "too long",
		},
		{
			name:       "uppercase",
			id:         "MyDB",
			wantErrMsg: "invalid",
		},
		{
			name:       "starts with hyphen",
			id:         "-foo",
			wantErrMsg: "invalid",
		},
		{
			name:       "ends with hyphen",
			id:         "foo-",
			wantErrMsg: "invalid",
		},
		{
			name:       "consecutive hyphens",
			id:         "foo--bar",
			wantErrMsg: "invalid",
		},
		{
			name:       "contains underscore",
			id:         "foo_bar",
			wantErrMsg: "invalid",
		},
		{
			name:       "contains dot",
			id:         "foo.bar",
			wantErrMsg: "invalid",
		},
		{
			name:       "contains space",
			id:         "foo bar",
			wantErrMsg: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := pvmigrate.ValidateID(tt.id)
			if tt.wantErrMsg != "" {
				require.ErrorContains(t, err, tt.wantErrMsg)

				return
			}

			assert.NoError(t, err)
		})
	}
}
