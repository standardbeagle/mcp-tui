package roots_test

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/mcp/roots"
)

// TestParseFlag_NamedAbsolute parses the canonical "name=path" form and
// confirms the path is converted into a file:// URI verbatim.
func TestParseFlag_NamedAbsolute(t *testing.T) {
	r, err := roots.ParseFlag("home=/tmp/x")
	if err != nil {
		t.Fatalf("ParseFlag: %v", err)
	}
	if r.Name != "home" {
		t.Errorf("Name = %q, want %q", r.Name, "home")
	}
	if want := wantFileURI(t, "/tmp/x"); r.URI != want {
		t.Errorf("URI = %q, want %q", r.URI, want)
	}
	assertWellFormedFileURI(t, r.URI)
}

// TestParseFlag_UnnamedAbsolute confirms a bare absolute path is accepted

// wantFileURI is the file:// URI the package should produce for path on the
// current platform. Hard-coding "file:///tmp/x" only holds on POSIX: on Windows
// filepath.Abs("/tmp/x") yields a drive path such as D:\tmp\x.
func wantFileURI(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", path, err)
	}
	slashed := filepath.ToSlash(abs)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

// assertWellFormedFileURI pins the invariant the Windows bug violated: a file
// URI must carry no authority, so a drive letter can never become the host.
func assertWellFormedFileURI(t *testing.T, uri string) {
	t.Helper()
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", uri, err)
	}
	if parsed.Scheme != "file" {
		t.Errorf("URI %q has scheme %q, want file", uri, parsed.Scheme)
	}
	if parsed.Host != "" {
		t.Errorf("URI %q has host %q; a file URI must have an empty authority", uri, parsed.Host)
	}
}

// without a name and is converted to a file:// URI.
func TestParseFlag_UnnamedAbsolute(t *testing.T) {
	r, err := roots.ParseFlag("/tmp/x")
	if err != nil {
		t.Fatalf("ParseFlag: %v", err)
	}
	if r.Name != "" {
		t.Errorf("Name = %q, want empty", r.Name)
	}
	if want := wantFileURI(t, "/tmp/x"); r.URI != want {
		t.Errorf("URI = %q, want %q", r.URI, want)
	}
	assertWellFormedFileURI(t, r.URI)
}

// TestParseFlag_Relative confirms relative paths are resolved against the
// current working directory before being converted to a file:// URI.
func TestParseFlag_Relative(t *testing.T) {
	r, err := roots.ParseFlag("docs=./mcp-tui-roots-test-rel")
	if err != nil {
		t.Fatalf("ParseFlag: %v", err)
	}
	if r.Name != "docs" {
		t.Errorf("Name = %q, want %q", r.Name, "docs")
	}
	if !strings.HasPrefix(r.URI, "file://") {
		t.Errorf("URI = %q, want file:// prefix", r.URI)
	}
	if !strings.HasSuffix(r.URI, "/mcp-tui-roots-test-rel") {
		t.Errorf("URI = %q, want suffix matching the input", r.URI)
	}
}

// TestParseFlag_AlreadyFileURI confirms an explicit file:// URI is passed
// through unchanged (modulo whitespace trimming).
func TestParseFlag_AlreadyFileURI(t *testing.T) {
	r, err := roots.ParseFlag("etc=file:///etc")
	if err != nil {
		t.Fatalf("ParseFlag: %v", err)
	}
	if r.URI != "file:///etc" {
		t.Errorf("URI = %q, want %q", r.URI, "file:///etc")
	}
}

// TestParseFlag_RejectsNonFileScheme confirms non-file URIs are rejected
// because the MCP spec currently only permits file://.
func TestParseFlag_RejectsNonFileScheme(t *testing.T) {
	_, err := roots.ParseFlag("api=https://example.com/x")
	if err == nil {
		t.Fatalf("ParseFlag: expected error for non-file scheme, got nil")
	}
	if !strings.Contains(err.Error(), "file://") {
		t.Errorf("error message %q does not mention file://", err.Error())
	}
}

// TestParseFlag_EmptySpec rejects an empty spec with a clear error.
func TestParseFlag_EmptySpec(t *testing.T) {
	if _, err := roots.ParseFlag(""); err == nil {
		t.Fatalf("ParseFlag(\"\"): expected error, got nil")
	}
	if _, err := roots.ParseFlag("   "); err == nil {
		t.Fatalf("ParseFlag(whitespace): expected error, got nil")
	}
}

// TestParseFlag_EmptyPathAfterEquals rejects "name=" (an empty path).
func TestParseFlag_EmptyPathAfterEquals(t *testing.T) {
	if _, err := roots.ParseFlag("name="); err == nil {
		t.Fatalf("ParseFlag(\"name=\"): expected error, got nil")
	}
}

// TestParseFlags_SkipsEmpty confirms ParseFlags ignores empty entries (which
// cobra StringSliceVar can produce on trailing commas) and accumulates the
// rest in order.
func TestParseFlags_SkipsEmpty(t *testing.T) {
	roots, err := roots.ParseFlags([]string{"a=/tmp/a", "", "b=/tmp/b"})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if len(roots) != 2 {
		t.Fatalf("len(roots) = %d, want 2", len(roots))
	}
	if roots[0].Name != "a" || roots[1].Name != "b" {
		t.Errorf("roots = %+v, want [a, b]", roots)
	}
}

// TestParseFlags_PropagatesError confirms the first parse error stops the
// loop and is returned, so users see the bad spec rather than a partial list.
func TestParseFlags_PropagatesError(t *testing.T) {
	_, err := roots.ParseFlags([]string{"a=/tmp/a", "bad=https://x"})
	if err == nil {
		t.Fatalf("ParseFlags: expected error, got nil")
	}
}

// TestLoadFile_HappyPath parses a well-formed roots file and converts each
// entry into an officialMCP.Root with a file:// URI.
func TestLoadFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roots.json")
	body := `{
		"roots": [
			{"name": "home", "uri": "/tmp/home"},
			{"name": "etc",  "uri": "file:///etc"}
		]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := roots.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if want := wantFileURI(t, "/tmp/home"); got[0].Name != "home" || got[0].URI != want {
		t.Errorf("got[0] = %+v, want {home, %s}", got[0], want)
	}
	assertWellFormedFileURI(t, got[0].URI)
	if got[1].Name != "etc" || got[1].URI != "file:///etc" {
		t.Errorf("got[1] = %+v, want {etc, file:///etc}", got[1])
	}
}

// TestLoadFile_EmptyURI rejects an entry with an empty URI.
func TestLoadFile_EmptyURI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roots.json")
	body := `{"roots": [{"name": "x", "uri": ""}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := roots.LoadFile(path); err == nil {
		t.Fatalf("LoadFile: expected error for empty uri, got nil")
	}
}

// TestLoadFile_BadJSON rejects malformed input with a clear error.
func TestLoadFile_BadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roots.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := roots.LoadFile(path); err == nil {
		t.Fatalf("LoadFile: expected error for bad json, got nil")
	}
}

// TestLoadFile_MissingFile returns an error with the file path so the user
// can fix the typo.
func TestLoadFile_MissingFile(t *testing.T) {
	_, err := roots.LoadFile("/this/path/does/not/exist.json")
	if err == nil {
		t.Fatalf("LoadFile: expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "/this/path/does/not/exist.json") {
		t.Errorf("error message %q does not mention the path", err.Error())
	}
}

// TestLoadFile_EmptyPath rejects an empty path argument up front.
func TestLoadFile_EmptyPath(t *testing.T) {
	if _, err := roots.LoadFile(""); err == nil {
		t.Fatalf("LoadFile(\"\"): expected error, got nil")
	}
}

// TestLoadFile_EmptyRoots is allowed: an empty roots array is a valid (if
// useless) config and produces a zero-length slice.
func TestLoadFile_EmptyRoots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roots.json")
	if err := os.WriteFile(path, []byte(`{"roots": []}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := roots.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}
