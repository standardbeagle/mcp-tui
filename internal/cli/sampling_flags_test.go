package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newCmdWithSamplingFlags returns a child cobra.Command nested under a root
// that defines the same persistent flags as the real CLI. Using a parent/
// child pair mirrors how main.go wires the flags onto rootCmd; calling
// cmd.Flags() then resolves inherited persistent flags the same way the real
// PreRunE does at runtime.
func newCmdWithSamplingFlags() *cobra.Command {
	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().Bool("debug", false, "")
	root.PersistentFlags().String("sampling-stub", "", "")
	root.PersistentFlags().String("sampling-stub-file", "", "")

	child := &cobra.Command{Use: "child"}
	root.AddCommand(child)

	// Force inherited flag set initialization on the child the same way cobra
	// does it during Execute. Without this, cmd.Flags() on a child that has
	// never been executed returns only its local flag set.
	_ = child.ParseFlags(nil)
	return child
}

// TestSetupService_NoSamplingFlags_NoError verifies the default code path
// (no sampling flags) does not error and creates a service.
func TestSetupService_NoSamplingFlags_NoError(t *testing.T) {
	c := NewBaseCommand()
	cmd := newCmdWithSamplingFlags()

	if err := c.setupService(cmd, true); err != nil {
		t.Fatalf("setupService: %v", err)
	}
	if c.service == nil {
		t.Fatal("service was not created")
	}
}

// TestSetupService_SamplingStubText_NoError verifies --sampling-stub parses
// and registers without error. Round-trip behavior is covered by the
// integration test in internal/mcp/sampling.
func TestSetupService_SamplingStubText_NoError(t *testing.T) {
	c := NewBaseCommand()
	cmd := newCmdWithSamplingFlags()
	if err := cmd.Root().PersistentFlags().Set("sampling-stub", "ok"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	if err := c.setupService(cmd, true); err != nil {
		t.Fatalf("setupService: %v", err)
	}
}

// TestSetupService_SamplingStubFile_NoError verifies --sampling-stub-file
// reads and parses a JSON template without error.
func TestSetupService_SamplingStubFile_NoError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stub.json")
	if err := os.WriteFile(path, []byte(`{"text":"file ok"}`), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	c := NewBaseCommand()
	cmd := newCmdWithSamplingFlags()
	if err := cmd.Root().PersistentFlags().Set("sampling-stub-file", path); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	if err := c.setupService(cmd, true); err != nil {
		t.Fatalf("setupService: %v", err)
	}
}

// TestSetupService_BothSamplingFlags_Errors verifies the two flags are
// mutually exclusive.
func TestSetupService_BothSamplingFlags_Errors(t *testing.T) {
	c := NewBaseCommand()
	cmd := newCmdWithSamplingFlags()
	_ = cmd.Root().PersistentFlags().Set("sampling-stub", "x")
	_ = cmd.Root().PersistentFlags().Set("sampling-stub-file", "/no/such/file")

	err := c.setupService(cmd, true)
	if err == nil {
		t.Fatal("expected error when both flags are set, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutually exclusive error, got: %v", err)
	}
}

// TestSetupService_SamplingStubFileMissing_Errors verifies a missing stub
// file produces a useful error rather than a silent fallback.
func TestSetupService_SamplingStubFileMissing_Errors(t *testing.T) {
	c := NewBaseCommand()
	cmd := newCmdWithSamplingFlags()
	_ = cmd.Root().PersistentFlags().Set("sampling-stub-file", "/no/such/path/stub.json")

	err := c.setupService(cmd, true)
	if err == nil {
		t.Fatal("expected error for missing stub file, got nil")
	}
}
