package debug

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/standardbeagle/mcp-tui/internal/redact"
)

// NewSlogHandler returns a slog.Handler that writes into the global debug
// logger under the given component, so libraries that log through slog (the
// MCP SDK's ClientOptions.Logger) land in the same stderr stream and TUI Logs
// tab as mcp-tui's own lines, filtered by the same --log-level.
//
// Levels map onto the nearest LogLevel at or below the slog level. Attributes
// become fields; groups prefix their members' keys with "group.". Values pass
// through the logger core's key-based redaction, and error values have any
// URL in them masked, because SDK transport errors quote full request URLs.
func NewSlogHandler(component string) slog.Handler {
	return &slogHandler{component: component}
}

// SlogLogger is slog.New(NewSlogHandler(component)).
func SlogLogger(component string) *slog.Logger {
	return slog.New(NewSlogHandler(component))
}

type slogHandler struct {
	component string
	fields    []Field // from WithAttrs, keys already group-prefixed
	prefix    string  // "a.b." from WithGroup
}

func (h *slogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return globalEnabled(logLevelFromSlog(level))
}

//nolint:gocritic // hugeParam: the slog.Handler interface fixes this signature
func (h *slogHandler) Handle(_ context.Context, r slog.Record) error {
	fields := make([]Field, 0, len(h.fields)+r.NumAttrs())
	fields = append(fields, h.fields...)
	r.Attrs(func(a slog.Attr) bool {
		fields = appendSlogAttr(fields, h.prefix, a)
		return true
	})
	l, ok := Component(h.component).(*logger)
	if !ok {
		return fmt.Errorf("debug: component logger is %T, not the built-in logger", Component(h.component))
	}
	// The record's PC is the SDK's call site; this bridge is not.
	l.logFrom(func() string { return pcCallSite(r.PC) }, logLevelFromSlog(r.Level), r.Message, fields...)
	return nil
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	fields := make([]Field, 0, len(h.fields)+len(attrs))
	fields = append(fields, h.fields...)
	for _, a := range attrs {
		fields = appendSlogAttr(fields, h.prefix, a)
	}
	return &slogHandler{component: h.component, fields: fields, prefix: h.prefix}
}

func (h *slogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &slogHandler{component: h.component, fields: h.fields, prefix: h.prefix + name + "."}
}

// appendSlogAttr flattens a (possibly group-valued) attribute into fields.
func appendSlogAttr(fields []Field, prefix string, a slog.Attr) []Field {
	v := a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return fields
	}
	if v.Kind() == slog.KindGroup {
		groupPrefix := prefix
		if a.Key != "" {
			groupPrefix = prefix + a.Key + "."
		}
		for _, member := range v.Group() {
			fields = appendSlogAttr(fields, groupPrefix, member)
		}
		return fields
	}
	value := v.Any()
	if err, ok := value.(error); ok {
		value = redact.Error(err)
	}
	return append(fields, Field{Key: prefix + a.Key, Value: value})
}

// logLevelFromSlog maps a slog level to the nearest LogLevel at or below it.
func logLevelFromSlog(level slog.Level) LogLevel {
	switch {
	case level >= slog.LevelError:
		return LogLevelError
	case level >= slog.LevelWarn:
		return LogLevelWarn
	case level >= slog.LevelInfo:
		return LogLevelInfo
	default:
		return LogLevelDebug
	}
}

// globalEnabled reports whether the global logger would emit level.
func globalEnabled(level LogLevel) bool {
	l, ok := globalLogger.(*logger)
	if !ok {
		return true
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return level >= l.level
}
