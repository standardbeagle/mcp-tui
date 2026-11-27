package config

import (
	"fmt"
	"os"
	"regexp"
)

// envRef matches a ${NAME} reference. Only the braced form is expanded, so a
// bare "$" in a header value or URL (a password, a query string) is left
// alone.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnvRefs replaces every ${NAME} in value with the NAME environment
// variable. A variable that is unset or empty is an error, not an empty
// string: a saved "Bearer ${MCP_TOKEN}" must never go out as "Bearer ".
func ExpandEnvRefs(value string) (string, error) {
	var missing string
	expanded := envRef.ReplaceAllStringFunc(value, func(ref string) string {
		name := envRef.FindStringSubmatch(ref)[1]
		v := os.Getenv(name)
		if v == "" && missing == "" {
			missing = name
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("${%s} is not set", missing)
	}
	return expanded, nil
}
