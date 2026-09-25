package inputschema

import (
	"errors"
	"fmt"
)

// MaxArgumentKeyLength caps an argument name given on the command line.
const MaxArgumentKeyLength = 1000

// CheckArgumentKey reports whether key can name an argument on the command
// line: 1 to MaxArgumentKeyLength ASCII letters, digits, _ and -. The CLI's
// tool call and prompt get take only such keys, and the TUI checks it before
// copying a call as a command.
func CheckArgumentKey(key string) error {
	if key == "" {
		return errors.New("argument key is empty")
	}
	if len(key) > MaxArgumentKeyLength {
		return fmt.Errorf("argument key too long (max %d characters)", MaxArgumentKeyLength)
	}
	for _, r := range key {
		if !isArgumentKeyRune(r) {
			return fmt.Errorf("argument key contains invalid character: %q", r)
		}
	}
	return nil
}

func isArgumentKeyRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
}
