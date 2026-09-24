package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// The go-sdk v1.8.0 client removes every tool whose x-mcp-header
// annotations are invalid (SEP-2243) from tools/list results on all
// protocol versions, and only logs it, with these messages. The "tool" and
// "error" attributes name the tool and the reason.
const (
	sdkMsgExcludingTool    = "excluding tool from tools/list"
	sdkMsgExcludingNilTool = "excluding nil tool from tools/list"
	sdkAttrTool            = "tool"
	sdkAttrError           = "error"
)

// DroppedTool is a tool the server listed but the SDK client removed from
// tools/list, with the SDK's reason.
type DroppedTool struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// String renders "name (reason)"; a null entry in the server's list has no
// name.
func (d DroppedTool) String() string {
	name := d.Name
	if name == "" {
		name = "<null tool>"
	}
	return fmt.Sprintf("%s (%s)", name, d.Reason)
}

// sdkLogTap passes every SDK log record on to next and reports each tool
// the SDK dropped from a tools/list result to onDroppedTool, whatever the
// log level.
type sdkLogTap struct {
	next          slog.Handler
	onDroppedTool func(DroppedTool)
}

func (h *sdkLogTap) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelError || h.next.Enabled(ctx, level)
}

//nolint:gocritic // slog.Handler passes the record by value.
func (h *sdkLogTap) Handle(ctx context.Context, r slog.Record) error {
	switch r.Message {
	case sdkMsgExcludingTool:
		dropped := DroppedTool{}
		r.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case sdkAttrTool:
				dropped.Name = a.Value.String()
			case sdkAttrError:
				dropped.Reason = a.Value.String()
			}
			return true
		})
		h.onDroppedTool(dropped)
	case sdkMsgExcludingNilTool:
		h.onDroppedTool(DroppedTool{Reason: "the server listed a null tool"})
	}
	if !h.next.Enabled(ctx, r.Level) {
		return nil
	}
	return h.next.Handle(ctx, r)
}

func (h *sdkLogTap) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sdkLogTap{next: h.next.WithAttrs(attrs), onDroppedTool: h.onDroppedTool}
}

func (h *sdkLogTap) WithGroup(name string) slog.Handler {
	return &sdkLogTap{next: h.next.WithGroup(name), onDroppedTool: h.onDroppedTool}
}

// sdkLogger is the ClientOptions.Logger of every client: the SDK's slog
// output joins the debug log as "sdk", and dropped tools are recorded for
// DroppedTools.
func (s *service) sdkLogger() *slog.Logger {
	return slog.New(&sdkLogTap{next: debug.NewSlogHandler("sdk"), onDroppedTool: s.recordDroppedTool})
}

func (s *service) recordDroppedTool(dropped DroppedTool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.droppedInList = append(s.droppedInList, dropped)
}

// beginToolsList starts collecting the drops of one ListTools.
func (s *service) beginToolsList() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.droppedInList = nil
}

// endToolsList publishes the drops collected since beginToolsList. A list
// served entirely from the SDK cache (2026-07-28) was filtered when it was
// fetched, so its drops stay the ones already published.
func (s *service) endToolsList(info *ListCacheInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if info.FromCache() {
		return
	}
	s.droppedTools = s.droppedInList
	s.droppedInList = nil
	if len(s.droppedTools) > 0 {
		debug.Warn("SDK dropped tools from tools/list",
			debug.F("count", len(s.droppedTools)), debug.F("tools", s.droppedTools))
	}
}

// DroppedTools returns the tools the SDK removed from the most recent
// tools/list, or nil when it removed none or nothing was listed yet.
func (s *service) DroppedTools() []DroppedTool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.droppedTools) == 0 {
		return nil
	}
	return append([]DroppedTool(nil), s.droppedTools...)
}
