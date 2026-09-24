// Package tasks is mcp-tui's client for MCP tasks: durable handles a server
// returns in place of a tools/call result, which the client polls for the
// eventual result.
//
// Two incompatible forms exist, chosen by the negotiated protocol version:
//
//   - 2025-11-25 experimental tasks (SEP-1686): the client opts in per call
//     with a "task" parameter, the server declares capabilities.tasks, and the
//     client uses tasks/get, tasks/result, tasks/list and tasks/cancel.
//     https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks
//   - the io.modelcontextprotocol/tasks extension from 2026-07-28 (SEP-2663):
//     the client declares the extension in each request's capabilities, the
//     server decides per request whether to answer with a task, and the
//     client uses tasks/get, tasks/update and tasks/cancel.
//     https://github.com/modelcontextprotocol/ext-tasks/blob/main/specification/2026-07-28/tasks.md
//
// go-sdk v1.8.0 has neither, so this package speaks raw JSON-RPC. sdk.go is
// the only file that touches the SDK; replace it when go-sdk ships tasks.
package tasks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// ExtensionID names the 2026-07-28 tasks extension in capabilities.
const ExtensionID = "io.modelcontextprotocol/tasks"

// Form is the tasks protocol a session speaks.
type Form string

const (
	// FormNone: the protocol version predates tasks.
	FormNone Form = "none"
	// FormExperimental is the 2025-11-25 experimental feature.
	FormExperimental Form = "experimental"
	// FormExtension is the io.modelcontextprotocol/tasks extension.
	FormExtension Form = "extension"
)

// experimentalVersion is the only protocol version with experimental tasks;
// extensionVersion is the first with the extension.
const (
	experimentalVersion = "2025-11-25"
	extensionVersion    = "2026-07-28"
)

// FormFor returns the tasks form of a negotiated protocol version. Versions
// are ISO dates, so they order as strings.
func FormFor(protocolVersion string) Form {
	switch {
	case protocolVersion >= extensionVersion:
		return FormExtension
	case protocolVersion == experimentalVersion:
		return FormExperimental
	default:
		return FormNone
	}
}

// Status is a task's execution state.
type Status string

const (
	StatusWorking       Status = "working"
	StatusInputRequired Status = "input_required"
	StatusCompleted     Status = "completed"
	StatusFailed        Status = "failed"
	StatusCancelled     Status = "cancelled"
)

// IsTerminal reports whether s is completed, failed or cancelled; a task in
// a terminal status never changes again.
func (s Status) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusCancelled
}

func (s Status) known() bool {
	return s == StatusWorking || s == StatusInputRequired || s.IsTerminal()
}

// DefaultPollInterval is how often a client polls a task whose server gave
// no poll interval.
const DefaultPollInterval = time.Second

// Task is one task as the server last reported it, with both wire forms
// normalized to the extension's field names.
type Task struct {
	ID            string    `json:"taskId"`
	Status        Status    `json:"status"`
	StatusMessage string    `json:"statusMessage,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	LastUpdatedAt time.Time `json:"lastUpdatedAt"`
	// TTLMs is the retention from creation in milliseconds; nil is unlimited.
	TTLMs *int64 `json:"ttlMs"`
	// PollIntervalMs is the server's suggested polling interval, if any.
	PollIntervalMs *int64 `json:"pollIntervalMs,omitempty"`
	// InputRequests (extension, input_required) are the outstanding
	// server-to-client requests, keyed by the server.
	InputRequests json.RawMessage `json:"inputRequests,omitempty"`
	// Result (extension, completed) is the underlying request's result.
	Result json.RawMessage `json:"result,omitempty"`
	// Error (extension, failed) is the JSON-RPC error the request failed with.
	Error *RPCError `json:"error,omitempty"`
}

// Expired reports whether the task outlived its TTL without reaching a
// terminal status, the point from which the spec lets a client treat it as
// no longer usable.
func (t *Task) Expired(now time.Time) bool {
	if t.TTLMs == nil || t.Status.IsTerminal() {
		return false
	}
	return now.After(t.CreatedAt.Add(time.Duration(*t.TTLMs) * time.Millisecond))
}

// PollInterval is the server's suggested polling interval, or
// DefaultPollInterval when it gave none.
func (t *Task) PollInterval() time.Duration {
	if t.PollIntervalMs == nil || *t.PollIntervalMs <= 0 {
		return DefaultPollInterval
	}
	return time.Duration(*t.PollIntervalMs) * time.Millisecond
}

// InputRequestKeys lists the task's outstanding input requests as sorted
// "key (method)" entries.
func (t *Task) InputRequestKeys() ([]string, error) {
	var requests map[string]struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(t.InputRequests, &requests); err != nil {
		return nil, fmt.Errorf("decoding inputRequests: %w", err)
	}
	keys := make([]string, 0, len(requests))
	for key, r := range requests {
		keys = append(keys, key+" ("+r.Method+")")
	}
	sort.Strings(keys)
	return keys, nil
}

// Fingerprint is a short, stable stand-in for a task ID in logs. A server
// may use task IDs as bearer tokens, so the ID itself never reaches a log.
func Fingerprint(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:4])
}

// RPCError is a JSON-RPC error: one a task failed with, or one a tasks
// request was answered with.
type RPCError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("%s (JSON-RPC %d)", e.Message, e.Code)
}

// ErrMalformedTask marks a task the server described in violation of the
// spec: a missing or unknown field, or a status without its payload.
var ErrMalformedTask = errors.New("malformed task")

// wireTask carries the fields of both forms.
type wireTask struct {
	ID             string          `json:"taskId"`
	Status         Status          `json:"status"`
	StatusMessage  string          `json:"statusMessage"`
	CreatedAt      string          `json:"createdAt"`
	LastUpdatedAt  string          `json:"lastUpdatedAt"`
	TTL            *int64          `json:"ttl"`
	PollInterval   *int64          `json:"pollInterval"`
	TTLMs          *int64          `json:"ttlMs"`
	PollIntervalMs *int64          `json:"pollIntervalMs"`
	InputRequests  json.RawMessage `json:"inputRequests"`
	Result         json.RawMessage `json:"result"`
	Error          *RPCError       `json:"error"`
}

// DecodeTask decodes a task as form puts it on the wire: a tasks/get
// result, a notification's params, or an experimental tasks/list entry.
func DecodeTask(form Form, raw json.RawMessage) (Task, error) {
	var w wireTask
	if err := json.Unmarshal(raw, &w); err != nil {
		return Task{}, fmt.Errorf("%w: %v", ErrMalformedTask, err)
	}
	if w.ID == "" {
		return Task{}, fmt.Errorf("%w: no taskId", ErrMalformedTask)
	}
	if !w.Status.known() {
		return Task{}, fmt.Errorf("%w: unknown status %q", ErrMalformedTask, w.Status)
	}
	created, err := parseTime("createdAt", w.CreatedAt)
	if err != nil {
		return Task{}, err
	}
	updated, err := parseTime("lastUpdatedAt", w.LastUpdatedAt)
	if err != nil {
		return Task{}, err
	}
	t := Task{
		ID:             w.ID,
		Status:         w.Status,
		StatusMessage:  w.StatusMessage,
		CreatedAt:      created,
		LastUpdatedAt:  updated,
		TTLMs:          w.TTL,
		PollIntervalMs: w.PollInterval,
	}
	if form == FormExtension {
		t.TTLMs, t.PollIntervalMs = w.TTLMs, w.PollIntervalMs
		t.InputRequests, t.Result, t.Error = nonNull(w.InputRequests), nonNull(w.Result), w.Error
		if err := checkStatusPayload(&t); err != nil {
			return Task{}, err
		}
	}
	return t, nil
}

// checkStatusPayload enforces the extension's status-specific fields.
func checkStatusPayload(t *Task) error {
	switch {
	case t.Status == StatusInputRequired && len(t.InputRequests) == 0:
		return fmt.Errorf("%w: input_required without inputRequests", ErrMalformedTask)
	case t.Status == StatusCompleted && len(t.Result) == 0:
		return fmt.Errorf("%w: completed without result", ErrMalformedTask)
	case t.Status == StatusFailed && t.Error == nil:
		return fmt.Errorf("%w: failed without error", ErrMalformedTask)
	}
	return nil
}

func parseTime(field, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("%w: no %s", ErrMalformedTask, field)
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %s is not an ISO 8601 timestamp: %q", ErrMalformedTask, field, value)
	}
	return t, nil
}

// nonNull drops a JSON null so an absent and a null payload read the same.
func nonNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	return raw
}

// resultTypeTask is the extension's discriminator for a CreateTaskResult.
const resultTypeTask = "task"

// DecodeCreateTaskResult returns the task a tools/call result hands back, or
// nil when the result is an ordinary (complete or input-required) result.
// The experimental form wraps the task in "task"; the extension flags it
// with resultType "task" and inlines it.
func DecodeCreateTaskResult(form Form, raw json.RawMessage) (*Task, error) {
	var probe struct {
		ResultType string          `json:"resultType"`
		Task       json.RawMessage `json:"task"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("decoding tools/call result: %w", err)
	}
	var taskRaw json.RawMessage
	switch form {
	case FormExperimental:
		taskRaw = nonNull(probe.Task)
	case FormExtension:
		if probe.ResultType == resultTypeTask {
			taskRaw = raw
		}
	}
	if taskRaw == nil {
		return nil, nil
	}
	t, err := DecodeTask(form, taskRaw)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Page is one page of an experimental tasks/list.
type Page struct {
	Tasks      []Task `json:"tasks"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// DecodeTaskPage decodes an experimental tasks/list result.
func DecodeTaskPage(raw json.RawMessage) (Page, error) {
	var w struct {
		Tasks      []json.RawMessage `json:"tasks"`
		NextCursor string            `json:"nextCursor"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return Page{}, fmt.Errorf("decoding tasks/list result: %w", err)
	}
	page := Page{Tasks: make([]Task, 0, len(w.Tasks)), NextCursor: w.NextCursor}
	for i, entry := range w.Tasks {
		t, err := DecodeTask(FormExperimental, entry)
		if err != nil {
			return Page{}, fmt.Errorf("tasks/list entry %d: %w", i, err)
		}
		page.Tasks = append(page.Tasks, t)
	}
	return page, nil
}
