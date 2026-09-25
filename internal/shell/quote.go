// Package shell writes command lines for a POSIX shell (sh, bash, zsh).
// Neither PowerShell nor cmd.exe reads them the same way.
package shell

import "strings"

// Quote returns s as one POSIX shell word that reads back as s: wrapped in
// single quotes, inside which the shell expands nothing ($, `, \ and
// newlines stay as they are), with each ' written as a closing quote, \'
// and an opening quote. A word of safe characters only is returned as is,
// to keep the common case readable.
func Quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\r\"'\\$`&|;<>()*?![]{}#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
