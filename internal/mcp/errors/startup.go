package errors

import "fmt"

// ServerStartupError reports a stdio server that failed during startup, with
// the stderr output that says why. The classifier recognizes it by type.
type ServerStartupError struct {
	Command    string
	Args       []string
	Output     string
	ExitCode   int
	Suggestion string
}

func (e *ServerStartupError) Error() string {
	if e.Suggestion != "" {
		return fmt.Sprintf("server startup failed: %s\n\nServer output:\n%s\n\nSuggestion: %s",
			e.Command, e.Output, e.Suggestion)
	}
	return fmt.Sprintf("server startup failed: %s\n\nServer output:\n%s",
		e.Command, e.Output)
}
