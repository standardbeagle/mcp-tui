package roots

import (
	"net/url"
	"testing"
)

// absPathToFileURI must produce a well-formed file URI on every platform.
//
// The Windows cases are the reason this function exists and is tested here
// rather than through ParseFlag: a drive path has no leading slash, so
// url.URL.String() would read "C:" as the authority and emit "file://C:/foo",
// silently sending a malformed root URI to the server. Testing the conversion
// directly exercises the Windows behaviour from any OS.
func TestAbsPathToFileURI(t *testing.T) {
	tests := []struct {
		name string
		abs  string
		want string
	}{
		{"unix absolute", "/tmp/x", "file:///tmp/x"},
		{"unix root", "/", "file:///"},
		// Drive paths are given already slashed: filepath.ToSlash only rewrites
		// backslashes on Windows, so a backslash input cannot be asserted from
		// a POSIX host. What matters here is the missing leading slash.
		{"windows drive", "C:/foo/bar", "file:///C:/foo/bar"},
		{"windows drive root", "D:/", "file:///D:/"},
		{"path with space", "/tmp/my docs", "file:///tmp/my%20docs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := absPathToFileURI(tt.abs)
			if got != tt.want {
				t.Errorf("absPathToFileURI(%q) = %q, want %q", tt.abs, got, tt.want)
			}

			// Whatever the platform, the drive letter must never become the host.
			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", got, err)
			}
			if parsed.Host != "" {
				t.Errorf("URI %q has host %q; a file URI must have an empty authority", got, parsed.Host)
			}
			if parsed.Scheme != "file" {
				t.Errorf("URI %q has scheme %q, want file", got, parsed.Scheme)
			}
		})
	}
}
