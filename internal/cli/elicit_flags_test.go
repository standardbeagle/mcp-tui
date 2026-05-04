package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newCmdWithElicitFlags returns a child cobra.Command nested under a root
// that defines the same persistent flags as the real CLI for elicitation.
// Mirrors newCmdWithSamplingFlags in sampling_flags_test.go.
func newCmdWithElicitFlags() *cobra.Command {
	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().Bool("debug", false, "")
	// Sampling flags are still defined because configureSamplingHandler
	// reads them from the same command; without these the test panics in
	// FlagSet lookup.
	root.PersistentFlags().String("sampling-stub", "", "")
	root.PersistentFlags().String("sampling-stub-file", "", "")
	root.PersistentFlags().String("sampling-tool-use", "", "")
	root.PersistentFlags().String("elicit-stub", "", "")
	root.PersistentFlags().String("elicit-stub-file", "", "")

	child := &cobra.Command{Use: "child"}
	root.AddCommand(child)
	_ = child.ParseFlags(nil)
	return child
}

// TestSetupService_ElicitStubJSON_NoError verifies --elicit-stub parses
// inline JSON and registers a handler without error.
func TestSetupService_ElicitStubJSON_NoError(t *testing.T) {
	c := NewBaseCommand()
	cmd := newCmdWithElicitFlags()
	if err := cmd.Root().PersistentFlags().Set("elicit-stub", `{"name":"alice"}`); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := c.setupService(cmd, true); err != nil {
		t.Fatalf("setupService: %v", err)
	}
}

// TestSetupService_ElicitStubFile_NoError verifies --elicit-stub-file reads
// a JSON template and registers without error.
func TestSetupService_ElicitStubFile_NoError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stub.json")
	if err := os.WriteFile(path, []byte(`{"endpoint":"https://x"}`), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	c := NewBaseCommand()
	cmd := newCmdWithElicitFlags()
	if err := cmd.Root().PersistentFlags().Set("elicit-stub-file", path); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := c.setupService(cmd, true); err != nil {
		t.Fatalf("setupService: %v", err)
	}
}

// TestSetupService_ElicitStubBadJSON_Errors verifies invalid JSON in the
// inline stub flag produces an error.
func TestSetupService_ElicitStubBadJSON_Errors(t *testing.T) {
	c := NewBaseCommand()
	cmd := newCmdWithElicitFlags()
	_ = cmd.Root().PersistentFlags().Set("elicit-stub", `{not json`)
	if err := c.setupService(cmd, true); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestSetupService_BothElicitFlags_Errors verifies the two flags are
// mutually exclusive.
func TestSetupService_BothElicitFlags_Errors(t *testing.T) {
	c := NewBaseCommand()
	cmd := newCmdWithElicitFlags()
	_ = cmd.Root().PersistentFlags().Set("elicit-stub", `{"x":1}`)
	_ = cmd.Root().PersistentFlags().Set("elicit-stub-file", "/no/such/file")
	err := c.setupService(cmd, true)
	if err == nil {
		t.Fatal("expected mutually-exclusive error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in error, got: %v", err)
	}
}

// TestSetupService_ElicitStubFileMissing_Errors verifies a missing file
// produces a clear error rather than a silent fallback.
func TestSetupService_ElicitStubFileMissing_Errors(t *testing.T) {
	c := NewBaseCommand()
	cmd := newCmdWithElicitFlags()
	_ = cmd.Root().PersistentFlags().Set("elicit-stub-file", "/no/such/path/elicit-stub.json")
	if err := c.setupService(cmd, true); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestSetupService_NoElicitFlags_NoError verifies the default code path
// (no elicit flags) does not error.
func TestSetupService_NoElicitFlags_NoError(t *testing.T) {
	c := NewBaseCommand()
	cmd := newCmdWithElicitFlags()
	if err := c.setupService(cmd, true); err != nil {
		t.Fatalf("setupService: %v", err)
	}
}
