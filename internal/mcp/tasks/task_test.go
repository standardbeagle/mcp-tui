package tasks

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestFormFor(t *testing.T) {
	cases := map[string]Form{
		"2024-11-05":        FormNone,
		"2025-06-18":        FormNone,
		experimentalVersion: FormExperimental,
		extensionVersion:    FormExtension,
		"2027-01-15":        FormExtension,
	}
	for version, want := range cases {
		if got := FormFor(version); got != want {
			t.Errorf("FormFor(%q) = %q, want %q", version, got, want)
		}
	}
}

// The two wire forms name the same fields differently (ttl/pollInterval
// against ttlMs/pollIntervalMs); both decode to the same Task.
func TestDecodeTask_BothForms(t *testing.T) {
	experimental := `{"taskId":"786512e2-9e0d-44bd-8f29-789f320fe840","status":"working",
		"statusMessage":"The operation is now in progress.","createdAt":"2025-11-25T10:30:00Z",
		"lastUpdatedAt":"2025-11-25T10:40:00Z","ttl":60000,"pollInterval":5000}`
	extension := `{"resultType":"complete","taskId":"786512e2-9e0d-44bd-8f29-789f320fe840","status":"working",
		"statusMessage":"The operation is now in progress.","createdAt":"2025-11-25T10:30:00Z",
		"lastUpdatedAt":"2025-11-25T10:40:00Z","ttlMs":60000,"pollIntervalMs":5000}`
	for form, raw := range map[Form]string{FormExperimental: experimental, FormExtension: extension} {
		t.Run(string(form), func(t *testing.T) {
			task, err := DecodeTask(form, json.RawMessage(raw))
			if err != nil {
				t.Fatal(err)
			}
			if task.ID != "786512e2-9e0d-44bd-8f29-789f320fe840" || task.Status != StatusWorking ||
				task.StatusMessage != "The operation is now in progress." {
				t.Errorf("task = %+v", task)
			}
			if task.TTLMs == nil || *task.TTLMs != 60000 || task.PollIntervalMs == nil || *task.PollIntervalMs != 5000 {
				t.Errorf("ttl/pollInterval = %v/%v", task.TTLMs, task.PollIntervalMs)
			}
			if !task.CreatedAt.Equal(time.Date(2025, 11, 25, 10, 30, 0, 0, time.UTC)) {
				t.Errorf("createdAt = %v", task.CreatedAt)
			}
		})
	}
}

// A null ttl means unlimited; it decodes to a nil TTLMs.
func TestDecodeTask_NullTTLIsUnlimited(t *testing.T) {
	task, err := DecodeTask(FormExtension, json.RawMessage(`{"taskId":"a1","status":"working",
		"createdAt":"2026-07-28T09:00:00Z","lastUpdatedAt":"2026-07-28T09:00:00Z","ttlMs":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if task.TTLMs != nil {
		t.Errorf("TTLMs = %d, want nil", *task.TTLMs)
	}
}

// The extension inlines status-specific payloads, and each is required
// for its status; a task missing one is a server spec violation.
func TestDecodeTask_ExtensionStatusPayloads(t *testing.T) {
	const head = `"taskId":"a1","createdAt":"2026-07-28T09:00:00Z","lastUpdatedAt":"2026-07-28T09:01:00Z","ttlMs":null`
	ok := map[string]func(Task) bool{
		`{` + head + `,"status":"completed","result":{"content":[{"type":"text","text":"Hello, Luca!"}],"isError":false}}`: func(t Task) bool {
			return len(t.Result) > 0
		},
		`{` + head + `,"status":"failed","error":{"code":-32603,"message":"API rate limit exceeded"}}`: func(t Task) bool {
			return t.Error != nil && t.Error.Code == -32603 && t.Error.Message == "API rate limit exceeded"
		},
		`{` + head + `,"status":"input_required","inputRequests":{"name":{"method":"elicitation/create","params":{"mode":"form","message":"Please enter your name.","requestedSchema":{"type":"object"}}}}}`: func(t Task) bool {
			return len(t.InputRequests) > 0
		},
	}
	for raw, check := range ok {
		task, err := DecodeTask(FormExtension, json.RawMessage(raw))
		if err != nil {
			t.Errorf("%s: %v", raw, err)
			continue
		}
		if !check(task) {
			t.Errorf("%s: payload not decoded: %+v", raw, task)
		}
	}
	for _, status := range []string{"completed", "failed", "input_required"} {
		_, err := DecodeTask(FormExtension, json.RawMessage(`{`+head+`,"status":"`+status+`"}`))
		if !errors.Is(err, ErrMalformedTask) {
			t.Errorf("%s without its payload: err = %v, want ErrMalformedTask", status, err)
		}
	}
}

func TestDecodeTask_RejectsMalformed(t *testing.T) {
	for name, raw := range map[string]string{
		"no taskId":      `{"status":"working","createdAt":"2026-07-28T09:00:00Z","lastUpdatedAt":"2026-07-28T09:00:00Z"}`,
		"unknown status": `{"taskId":"a1","status":"paused","createdAt":"2026-07-28T09:00:00Z","lastUpdatedAt":"2026-07-28T09:00:00Z"}`,
		"no createdAt":   `{"taskId":"a1","status":"working","lastUpdatedAt":"2026-07-28T09:00:00Z"}`,
		"bad createdAt":  `{"taskId":"a1","status":"working","createdAt":"yesterday","lastUpdatedAt":"2026-07-28T09:00:00Z"}`,
		"not an object":  `["a1"]`,
	} {
		if _, err := DecodeTask(FormExtension, json.RawMessage(raw)); !errors.Is(err, ErrMalformedTask) {
			t.Errorf("%s: err = %v, want ErrMalformedTask", name, err)
		}
	}
}

// A tools/call answer is a task handle only in the form's own shape: the
// experimental result wraps it in "task", the extension flags it with
// resultType "task".
func TestDecodeCreateTaskResult(t *testing.T) {
	const task = `"taskId":"t-42","status":"working","createdAt":"2026-07-28T09:00:00Z","lastUpdatedAt":"2026-07-28T09:00:00Z"`
	cases := []struct {
		form   Form
		raw    string
		isTask bool
	}{
		{FormExperimental, `{"task":{` + task + `,"ttl":60000}}`, true},
		{FormExperimental, `{"content":[{"type":"text","text":"72°F"}]}`, false},
		{FormExtension, `{"resultType":"task",` + task + `,"ttlMs":60000}`, true},
		{FormExtension, `{"resultType":"complete","content":[{"type":"text","text":"72°F"}]}`, false},
		{FormExtension, `{"content":[{"type":"text","text":"72°F"}]}`, false},
		{FormExtension, `{"resultType":"input_required","inputRequests":{}}`, false},
	}
	for _, c := range cases {
		got, err := DecodeCreateTaskResult(c.form, json.RawMessage(c.raw))
		if err != nil {
			t.Errorf("%s %s: %v", c.form, c.raw, err)
			continue
		}
		if (got != nil) != c.isTask {
			t.Errorf("%s %s: task = %v, want task %v", c.form, c.raw, got, c.isTask)
		}
		if got != nil && got.ID != "t-42" {
			t.Errorf("%s: taskId = %q", c.form, got.ID)
		}
	}
}

func TestDecodeTaskPage(t *testing.T) {
	page, err := DecodeTaskPage(json.RawMessage(`{"tasks":[
		{"taskId":"786512e2","status":"working","createdAt":"2025-11-25T10:30:00Z","lastUpdatedAt":"2025-11-25T10:40:00Z","ttl":30000,"pollInterval":5000},
		{"taskId":"abc123-def456","status":"completed","createdAt":"2025-11-25T09:15:00Z","lastUpdatedAt":"2025-11-25T10:40:00Z","ttl":60000}
	],"nextCursor":"next-page-cursor"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tasks) != 2 || page.Tasks[1].Status != StatusCompleted || page.NextCursor != "next-page-cursor" {
		t.Errorf("page = %+v", page)
	}
}

// Transitions follow the spec's state diagram: working and input_required
// move freely between each other and to any terminal status; a terminal
// status never changes.
func TestCanTransition(t *testing.T) {
	all := []Status{StatusWorking, StatusInputRequired, StatusCompleted, StatusFailed, StatusCancelled}
	for _, from := range all {
		for _, to := range all {
			want := from == to || !from.IsTerminal()
			if got := CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestTracker_RecordsTransitionsAndFlagsInvalidOnes(t *testing.T) {
	tr := NewTracker()
	at := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	taskAt := func(status Status) *Task {
		return &Task{ID: "t-42", Status: status, CreatedAt: at, LastUpdatedAt: at}
	}

	tn, err := tr.Observe(taskAt(StatusWorking))
	if err != nil || !tn.First || tn.To != StatusWorking {
		t.Fatalf("first observation = %+v, %v", tn, err)
	}
	tn, err = tr.Observe(taskAt(StatusWorking))
	if err != nil || tn.Changed() {
		t.Fatalf("repeat observation = %+v, %v", tn, err)
	}
	tn, err = tr.Observe(taskAt(StatusCompleted))
	if err != nil || tn.From != StatusWorking || tn.To != StatusCompleted || !tn.Changed() {
		t.Fatalf("working→completed = %+v, %v", tn, err)
	}
	if _, err = tr.Observe(taskAt(StatusWorking)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("completed→working: err = %v, want ErrInvalidTransition", err)
	}
	snap := tr.Snapshot()
	if len(snap) != 1 || snap[0].Status != StatusWorking {
		t.Errorf("snapshot keeps the latest server view even after a violation: %+v", snap)
	}
}

// TTL is the client's backstop: a task still not terminal after
// createdAt + ttl may be treated as unusable. A nil TTL never expires.
func TestExpired(t *testing.T) {
	created := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	ttl := int64(60000)
	working := Task{ID: "t", Status: StatusWorking, CreatedAt: created, TTLMs: &ttl}
	if working.Expired(created.Add(59 * time.Second)) {
		t.Error("expired before its ttl")
	}
	if !working.Expired(created.Add(61 * time.Second)) {
		t.Error("not expired after its ttl")
	}
	done := working
	done.Status = StatusCompleted
	if done.Expired(created.Add(time.Hour)) {
		t.Error("a terminal task never expires client-side")
	}
	unlimited := working
	unlimited.TTLMs = nil
	if unlimited.Expired(created.Add(24 * time.Hour)) {
		t.Error("a null ttl is unlimited")
	}
}

func TestPollInterval(t *testing.T) {
	five := int64(5000)
	if got := (&Task{PollIntervalMs: &five}).PollInterval(); got != 5*time.Second {
		t.Errorf("PollInterval = %v", got)
	}
	if got := (&Task{}).PollInterval(); got != DefaultPollInterval {
		t.Errorf("PollInterval without a server hint = %v, want %v", got, DefaultPollInterval)
	}
}

// Task IDs may be bearer tokens (SEP-2663 security considerations), so logs
// carry a stable fingerprint instead.
func TestFingerprint(t *testing.T) {
	id := "786512e2-9e0d-44bd-8f29-789f320fe840"
	fp := Fingerprint(id)
	if fp == id || len(fp) != 8 || fp != Fingerprint(id) || fp == Fingerprint(id+"x") {
		t.Errorf("Fingerprint(%q) = %q", id, fp)
	}
}
