package mcp

import (
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

// boolPtr returns a pointer to the given bool. Local helper because the
// production code never needs literal-bool pointers and we do not want to
// export this from the package.
func boolPtr(b bool) *bool { return &b }

// TestToolDisplayName covers the precedence Tool.Title → Annotations.Title →
// Tool.Name documented on Tool.DisplayName.
func TestToolDisplayName(t *testing.T) {
	cases := []struct {
		name string
		tool Tool
		want string
	}{
		{
			name: "name only",
			tool: Tool{Name: "echo"},
			want: "echo",
		},
		{
			name: "annotations title beats name",
			tool: Tool{
				Name:        "echo",
				Annotations: &ToolAnnotations{Title: "Echo Tool"},
			},
			want: "Echo Tool",
		},
		{
			name: "top-level title wins over annotations title",
			tool: Tool{
				Name:        "echo",
				Title:       "Top Echo",
				Annotations: &ToolAnnotations{Title: "Annotated Echo"},
			},
			want: "Top Echo",
		},
		{
			name: "empty annotations title falls through to name",
			tool: Tool{
				Name:        "echo",
				Annotations: &ToolAnnotations{Title: ""},
			},
			want: "echo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.tool.DisplayName())
		})
	}
}

// TestToolIsDestructive validates the gating decision used by the confirm
// modal and the CLI prompt.
func TestToolIsDestructive(t *testing.T) {
	cases := []struct {
		name string
		tool Tool
		want bool
	}{
		{
			name: "no annotations is not destructive",
			tool: Tool{Name: "x"},
			want: false,
		},
		{
			name: "destructiveHint nil is not destructive (mcp-tui no-surprise rule)",
			tool: Tool{Annotations: &ToolAnnotations{}},
			want: false,
		},
		{
			name: "destructiveHint=true triggers gate",
			tool: Tool{Annotations: &ToolAnnotations{DestructiveHint: boolPtr(true)}},
			want: true,
		},
		{
			name: "destructiveHint=false explicit",
			tool: Tool{Annotations: &ToolAnnotations{DestructiveHint: boolPtr(false)}},
			want: false,
		},
		{
			name: "readOnly suppresses destructive even when set",
			tool: Tool{Annotations: &ToolAnnotations{
				ReadOnlyHint:    true,
				DestructiveHint: boolPtr(true),
			}},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.tool.IsDestructive())
		})
	}
}

// TestToolHintAccessors covers IsReadOnly, IsIdempotent, IsOpenWorld.
func TestToolHintAccessors(t *testing.T) {
	t.Run("readOnly", func(t *testing.T) {
		assert.False(t, Tool{}.IsReadOnly())
		assert.False(t, Tool{Annotations: &ToolAnnotations{}}.IsReadOnly())
		assert.True(t, Tool{Annotations: &ToolAnnotations{ReadOnlyHint: true}}.IsReadOnly())
	})
	t.Run("idempotent", func(t *testing.T) {
		assert.False(t, Tool{}.IsIdempotent())
		assert.False(t, Tool{Annotations: &ToolAnnotations{}}.IsIdempotent())
		assert.True(t, Tool{Annotations: &ToolAnnotations{IdempotentHint: true}}.IsIdempotent())
	})
	t.Run("openWorld", func(t *testing.T) {
		assert.False(t, Tool{}.IsOpenWorld())
		assert.False(t, Tool{Annotations: &ToolAnnotations{}}.IsOpenWorld())
		assert.False(t, Tool{Annotations: &ToolAnnotations{OpenWorldHint: boolPtr(false)}}.IsOpenWorld())
		assert.True(t, Tool{Annotations: &ToolAnnotations{OpenWorldHint: boolPtr(true)}}.IsOpenWorld())
	})
}

// TestConvertToolAnnotations exercises the SDK→internal mapping including the
// nil-passthrough and the pointer copies (we must not alias the SDK fields).
func TestConvertToolAnnotations(t *testing.T) {
	t.Run("nil passes through", func(t *testing.T) {
		assert.Nil(t, convertToolAnnotations(nil))
	})

	t.Run("all fields populated", func(t *testing.T) {
		dt := true
		ow := false
		got := convertToolAnnotations(&officialMCP.ToolAnnotations{
			Title:           "Run Migration",
			ReadOnlyHint:    false,
			DestructiveHint: &dt,
			IdempotentHint:  true,
			OpenWorldHint:   &ow,
		})
		assert.NotNil(t, got)
		assert.Equal(t, "Run Migration", got.Title)
		assert.False(t, got.ReadOnlyHint)
		assert.NotNil(t, got.DestructiveHint)
		assert.True(t, *got.DestructiveHint)
		assert.True(t, got.IdempotentHint)
		assert.NotNil(t, got.OpenWorldHint)
		assert.False(t, *got.OpenWorldHint)
	})

	t.Run("pointer fields are copied not aliased", func(t *testing.T) {
		dt := true
		ow := true
		src := &officialMCP.ToolAnnotations{
			DestructiveHint: &dt,
			OpenWorldHint:   &ow,
		}
		got := convertToolAnnotations(src)
		// Mutate the source to ensure our copy is independent — defends
		// against future refactors that might accidentally alias the SDK
		// memory and produce racy reads.
		dt = false
		ow = false
		assert.True(t, *got.DestructiveHint, "DestructiveHint must be a copy")
		assert.True(t, *got.OpenWorldHint, "OpenWorldHint must be a copy")
	})
}

// TestRenderToolBadges verifies the badge string produced for the tool list.
// The function under test lives in the cli package so the assertion is on the
// internal helper which both CLI and TUI consume.
func TestToolBadgeString(t *testing.T) {
	cases := []struct {
		name string
		tool Tool
		want string
	}{
		{
			name: "no annotations renders empty",
			tool: Tool{Name: "x"},
			want: "",
		},
		{
			name: "destructive only",
			tool: Tool{Annotations: &ToolAnnotations{DestructiveHint: boolPtr(true)}},
			want: "[D]",
		},
		{
			name: "readOnly only",
			tool: Tool{Annotations: &ToolAnnotations{ReadOnlyHint: true}},
			want: "[R]",
		},
		{
			name: "idempotent only",
			tool: Tool{Annotations: &ToolAnnotations{IdempotentHint: true}},
			want: "[I]",
		},
		{
			name: "openWorld only",
			tool: Tool{Annotations: &ToolAnnotations{OpenWorldHint: boolPtr(true)}},
			want: "[O]",
		},
		{
			name: "destructive + idempotent + openWorld combined",
			tool: Tool{Annotations: &ToolAnnotations{
				DestructiveHint: boolPtr(true),
				IdempotentHint:  true,
				OpenWorldHint:   boolPtr(true),
			}},
			want: "[D][I][O]",
		},
		{
			name: "readOnly suppresses destructive in badge",
			tool: Tool{Annotations: &ToolAnnotations{
				ReadOnlyHint:    true,
				DestructiveHint: boolPtr(true),
			}},
			// Read-only wins; the destructive flag is suppressed (per IsDestructive).
			want: "[R]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.tool.BadgeString())
		})
	}
}
