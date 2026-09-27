package clipboard

import (
	"context"

	"github.com/atotto/clipboard"
)

// systemClipboard uses the win32 clipboard API through atotto, which takes
// no context; Clipboard's own deadline bounds it.
type systemClipboard struct{}

func (systemClipboard) Write(_ context.Context, text string) error { return clipboard.WriteAll(text) }
func (systemClipboard) Read(context.Context) (string, error)       { return clipboard.ReadAll() }
