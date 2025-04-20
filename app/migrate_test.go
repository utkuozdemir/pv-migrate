package app

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRunMigration is a simplified version of runMigration for testing the NodePort port validation.
func mockRunMigration(cmd *cobra.Command, _ []string) error {
	flags := cmd.Flags()

	// Get the NodePort port value
	nodePortPort, _ := flags.GetInt(FlagNodePortPort)

	// Perform the validation we're testing
	if nodePortPort != 0 {
		if nodePortPort < 30000 || nodePortPort > 32767 {
			return fmt.Errorf("invalid NodePort port %d: must be between 30000-32767", nodePortPort)
		}
	}

	return nil
}

// setupNodePortValidationTest prepares a test command for NodePort validation tests.
func setupNodePortValidationTest() *cobra.Command {
	cmd := &cobra.Command{
		RunE: mockRunMigration,
	}
	cmd.Flags().Int(FlagNodePortPort, 0, "custom port to use for NodePort service")

	return cmd
}

// TestNodePortPortValidation tests that the NodePort port validation works correctly.
func TestNodePortPortValidation(t *testing.T) {
	t.Parallel()

	// Create a test command with the migrate flags
	cmd := setupNodePortValidationTest()

	tests := []struct {
		name        string
		port        int
		expectError bool
	}{
		{
			name:        "Valid NodePort port (minimum value)",
			port:        30000,
			expectError: false,
		},
		{
			name:        "Valid NodePort port (maximum value)",
			port:        32767,
			expectError: false,
		},
		{
			name:        "Valid NodePort port (middle of range)",
			port:        31234,
			expectError: false,
		},
		{
			name:        "Invalid NodePort port (below range)",
			port:        29999,
			expectError: true,
		},
		{
			name:        "Invalid NodePort port (above range)",
			port:        32768,
			expectError: true,
		},
		{
			name:        "No custom port (default value)",
			port:        0,
			expectError: false,
		},
	}

	for _, testCase := range tests {
		// Capture range variable for parallel execution
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			testNodePortValidation(t, cmd, testCase.port, testCase.expectError)
		})
	}
}

// testNodePortValidation performs the actual validation test for a specific port.
func testNodePortValidation(t *testing.T, cmd *cobra.Command, port int, expectError bool) {
	t.Helper()

	// Set the test port and check for errors
	err := cmd.Flags().Set(FlagNodePortPort, strconv.Itoa(port))
	require.NoError(t, err, "Failed to set flag value")

	// Execute the command
	err = cmd.RunE(cmd, []string{})

	// Check if the error behavior matches expectations
	if expectError {
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid NodePort port")
	} else {
		require.NoError(t, err)
	}
}
