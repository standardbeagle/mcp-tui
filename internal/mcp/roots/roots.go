// Package roots provides user-declared root directory parsing and config-file
// loading for the MCP roots/list capability.
//
// MCP filesystem-aware servers need to know which directories the user has
// granted them. The client surfaces those via the SDK's roots feature: the
// SDK auto-handles roots/list requests from the server and dispatches
// roots/list_changed notifications when AddRoots/RemoveRoots is called.
//
// This package only handles the user-input side: parse a CLI --root flag,
// load a JSON file of roots, and produce officialMCP.Root values that callers
// (CLI base command, TUI screens) can hand to the service.
package roots

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ParseFlag parses a single `--root` flag value of the form "name=path" or
// just "path" (an unnamed root). The path is converted to an absolute file://
// URI as required by the MCP spec.
//
// Examples:
//
//	"home=/Users/me"       -> {Name: "home", URI: "file:///Users/me"}
//	"/tmp"                 -> {Name: "",     URI: "file:///tmp"}
//	"docs=./relative/path" -> {Name: "docs", URI: "file:///<cwd>/relative/path"}
//	"notes=file:///etc"    -> {Name: "notes", URI: "file:///etc"} (already a URI)
//
// Returns an error if the path cannot be made absolute or if the spec is
// otherwise malformed.
func ParseFlag(spec string) (*officialMCP.Root, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("root spec is empty")
	}

	var name, path string
	if idx := strings.Index(spec, "="); idx >= 0 {
		name = strings.TrimSpace(spec[:idx])
		path = strings.TrimSpace(spec[idx+1:])
		if path == "" {
			return nil, fmt.Errorf("root spec %q has empty path", spec)
		}
	} else {
		path = spec
	}

	uri, err := pathToFileURI(path)
	if err != nil {
		return nil, fmt.Errorf("root spec %q: %w", spec, err)
	}

	return &officialMCP.Root{Name: name, URI: uri}, nil
}

// ParseFlags parses a slice of --root flag values into roots. Empty entries
// are skipped (cobra StringSliceVar can produce empty values when the user
// passes a trailing comma). Returns the accumulated list and the first
// parse error encountered, if any.
func ParseFlags(specs []string) ([]*officialMCP.Root, error) {
	out := make([]*officialMCP.Root, 0, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec) == "" {
			continue
		}
		r, err := ParseFlag(spec)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// FileSchema is the JSON shape accepted by LoadFile. The schema mirrors the
// task spec: a top-level "roots" array of {name, uri} objects. Unknown fields
// are tolerated for forward compatibility.
type FileSchema struct {
	Roots []FileRoot `json:"roots"`
}

// FileRoot is a single entry in the roots config file.
type FileRoot struct {
	Name string `json:"name,omitempty"`
	URI  string `json:"uri"`
}

// LoadFile reads a JSON file of roots from path and returns the parsed roots.
// The file format is {"roots": [{"name": "x", "uri": "file:///..."}]}.
//
// URIs are normalized: bare paths are converted to file:// URIs (same rules
// as ParseFlag), and explicit file:// URIs are passed through. Any non-file
// scheme is rejected because the MCP spec currently only allows file://.
func LoadFile(path string) ([]*officialMCP.Root, error) {
	if path == "" {
		return nil, fmt.Errorf("roots file path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read roots file %q: %w", path, err)
	}

	var spec FileSchema
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse roots file %q: %w", path, err)
	}

	out := make([]*officialMCP.Root, 0, len(spec.Roots))
	for i, r := range spec.Roots {
		if strings.TrimSpace(r.URI) == "" {
			return nil, fmt.Errorf("roots file %q: entry %d has empty uri", path, i)
		}
		uri, err := pathToFileURI(r.URI)
		if err != nil {
			return nil, fmt.Errorf("roots file %q entry %d: %w", path, i, err)
		}
		out = append(out, &officialMCP.Root{Name: r.Name, URI: uri})
	}
	return out, nil
}

// absPathToFileURI converts an already-absolute OS path into a file:// URI.
//
// On Unix an absolute path already starts with "/". On Windows filepath.Abs
// produces a drive path such as C:\foo\bar, which slashes to "C:/foo/bar" --
// with no leading slash. url.URL.String() then reads the first segment as the
// authority and emits "file://C:/foo/bar", where "C:" is the *host*. A file URI
// needs "file:///C:/foo/bar", so the slash is added explicitly.
//
// It takes the path rather than deriving it so the Windows behaviour can be
// tested from any platform.
func absPathToFileURI(abs string) string {
	slashed := filepath.ToSlash(abs)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

// pathToFileURI normalizes a path-or-URI string into a file:// URI. Already-
// well-formed file:// URIs are passed through. Other schemes are rejected.
// Bare paths are made absolute (relative to the current working directory)
// and converted to file:// form.
func pathToFileURI(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("path is empty")
	}

	// Already a URI?
	if strings.Contains(input, "://") {
		u, err := url.Parse(input)
		if err != nil {
			return "", fmt.Errorf("parse uri %q: %w", input, err)
		}
		if u.Scheme != "file" {
			return "", fmt.Errorf("uri %q has scheme %q; only file:// is supported", input, u.Scheme)
		}
		return input, nil
	}

	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("resolve %q to absolute path: %w", input, err)
	}
	return absPathToFileURI(abs), nil
}
