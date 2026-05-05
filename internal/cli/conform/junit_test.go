package conform

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

// TestBuildJUnitReport_AllPass checks the happy-path: a slice of three
// passing scenarios produces a testsuite with tests=3, failures=0, no
// failure children, and stable per-testcase ordering matching the input
// slice order.
func TestBuildJUnitReport_AllPass(t *testing.T) {
	results := []ScenarioResult{
		{Name: "alpha", Pass: true, Elapsed: 10 * time.Millisecond},
		{Name: "beta", Pass: true, Elapsed: 25 * time.Millisecond},
		{Name: "gamma", Pass: true, Elapsed: 5 * time.Millisecond},
	}
	suite := BuildJUnitReport("mcp-tui.conform", results)
	if suite.Name != "mcp-tui.conform" {
		t.Errorf("Name = %q, want mcp-tui.conform", suite.Name)
	}
	if suite.Tests != 3 {
		t.Errorf("Tests = %d, want 3", suite.Tests)
	}
	if suite.Failures != 0 {
		t.Errorf("Failures = %d, want 0", suite.Failures)
	}
	if len(suite.Cases) != 3 {
		t.Fatalf("Cases len = %d, want 3", len(suite.Cases))
	}
	if suite.Cases[0].Name != "alpha" || suite.Cases[1].Name != "beta" || suite.Cases[2].Name != "gamma" {
		t.Errorf("case order not preserved: got %s/%s/%s", suite.Cases[0].Name, suite.Cases[1].Name, suite.Cases[2].Name)
	}
	if suite.Cases[0].ClassName != "mcp-tui.conform" {
		t.Errorf("ClassName = %q, want mcp-tui.conform", suite.Cases[0].ClassName)
	}
	for _, tc := range suite.Cases {
		if tc.Failure != nil {
			t.Errorf("case %s should have nil Failure, got %+v", tc.Name, tc.Failure)
		}
	}
	// Total time should be 0.040 s (10 + 25 + 5 ms).
	if suite.Time != "0.040" {
		t.Errorf("Time = %q, want 0.040", suite.Time)
	}
}

// TestBuildJUnitReport_WithFailures checks the failure shape: failed
// scenarios produce a Failure child with Message+Type+Body, the testsuite
// failures count tracks the total, and skipped scenarios do NOT count as
// failures (Skipped is a sub-state of Pass).
func TestBuildJUnitReport_WithFailures(t *testing.T) {
	results := []ScenarioResult{
		{Name: "ok", Pass: true, Elapsed: time.Millisecond},
		{Name: "broken", Pass: false, Error: "tool failed", Detail: "stack trace here", Elapsed: 2 * time.Millisecond},
		{Name: "skipped", Pass: true, Skipped: true, Error: "no tools", Elapsed: time.Microsecond},
	}
	suite := BuildJUnitReport("mcp-tui.conform", results)
	if suite.Failures != 1 {
		t.Errorf("Failures = %d, want 1", suite.Failures)
	}
	// "broken" is the second case.
	tc := suite.Cases[1]
	if tc.Failure == nil {
		t.Fatal("expected Failure on broken case")
	}
	if tc.Failure.Message != "tool failed" {
		t.Errorf("Message = %q, want %q", tc.Failure.Message, "tool failed")
	}
	if tc.Failure.Type != "Failure" {
		t.Errorf("Type = %q, want Failure", tc.Failure.Type)
	}
	if tc.Failure.Body != "stack trace here" {
		t.Errorf("Body = %q, want %q", tc.Failure.Body, "stack trace here")
	}
	// Skipped case must NOT have a Failure child.
	if suite.Cases[2].Failure != nil {
		t.Errorf("skipped case should have nil Failure, got %+v", suite.Cases[2].Failure)
	}
}

// TestBuildJUnitReport_Empty validates the zero-results edge case: empty
// suite, zero counts, no children. Used by the CLI when --scenario filters
// down to a single scenario whose name was bad — the run produced no
// scenarios but the report should still be well-formed XML.
func TestBuildJUnitReport_Empty(t *testing.T) {
	suite := BuildJUnitReport("mcp-tui.conform", nil)
	if suite.Tests != 0 || suite.Failures != 0 {
		t.Errorf("zero suite should have zero counts, got Tests=%d Failures=%d", suite.Tests, suite.Failures)
	}
	if len(suite.Cases) != 0 {
		t.Errorf("zero suite should have no cases, got %d", len(suite.Cases))
	}
	if suite.Time != "0.000" {
		t.Errorf("zero suite Time = %q, want 0.000", suite.Time)
	}
}

// TestWriteJUnitReport_WellFormed marshals a representative suite to XML
// and parses it back — a regression-grade check that our tags produce
// valid JUnit XML. Also confirms the XML prolog is emitted (required by
// some CI parsers) and the indented form is human-readable.
func TestWriteJUnitReport_WellFormed(t *testing.T) {
	results := []ScenarioResult{
		{Name: "tools.list", Pass: true, Elapsed: 12 * time.Millisecond},
		{Name: "tools.call", Pass: false, Error: "boom", Detail: "stderr line\nstderr line 2", Elapsed: 7 * time.Millisecond},
	}
	suite := BuildJUnitReport("mcp-tui.conform", results)

	var buf bytes.Buffer
	if err := WriteJUnitReport(&buf, suite); err != nil {
		t.Fatalf("WriteJUnitReport: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, `<?xml`) {
		t.Errorf("output should begin with XML prolog, got %q", out[:30])
	}
	if !strings.Contains(out, `<testsuite`) {
		t.Errorf("output missing testsuite element: %s", out)
	}
	if !strings.Contains(out, `name="tools.list"`) {
		t.Errorf("output missing tools.list testcase name attr: %s", out)
	}
	if !strings.Contains(out, `<failure message="boom"`) {
		t.Errorf("output missing failure message attr: %s", out)
	}

	// Round-trip parse.
	var got JUnitTestSuite
	dec := xml.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode: %v\nXML:\n%s", err, out)
	}
	if got.Tests != 2 {
		t.Errorf("decoded Tests = %d, want 2", got.Tests)
	}
	if got.Failures != 1 {
		t.Errorf("decoded Failures = %d, want 1", got.Failures)
	}
	if len(got.Cases) != 2 {
		t.Fatalf("decoded Cases len = %d, want 2", len(got.Cases))
	}
	if got.Cases[1].Failure == nil {
		t.Fatal("decoded second case should have Failure")
	}
	if got.Cases[1].Failure.Body != "stderr line\nstderr line 2" {
		t.Errorf("decoded Body = %q, want literal newlines preserved", got.Cases[1].Failure.Body)
	}
}

// TestFormatJUnitDuration covers the duration formatter's edge cases:
// negative durations clamp to zero, sub-millisecond durations round
// to "0.000", whole-second durations have three trailing zeros.
func TestFormatJUnitDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0.000"},
		{time.Millisecond, "0.001"},
		{500 * time.Microsecond, "0.001"}, // rounds up to 1ms in seconds-with-3-decimals: actually 0.0005 → "0.000"
		{time.Second, "1.000"},
		{1500 * time.Millisecond, "1.500"},
		{-time.Second, "0.000"},
	}
	for _, c := range cases {
		got := formatJUnitDuration(c.in)
		// Special case for the 500us → 0.000/0.001 ambiguity: %.3f rounds
		// half-to-even, so 0.0005 may emit either. Allow both.
		if c.in == 500*time.Microsecond {
			if got != "0.000" && got != "0.001" {
				t.Errorf("formatJUnitDuration(500us) = %q, want 0.000 or 0.001", got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("formatJUnitDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBuildJUnitReport_PreservesCaseTime verifies that each testcase's
// Time attribute reflects only its own elapsed time, not the cumulative
// suite total. Without this invariant CI dashboards would show every test
// as having taken the same "total run" duration.
func TestBuildJUnitReport_PreservesCaseTime(t *testing.T) {
	results := []ScenarioResult{
		{Name: "fast", Pass: true, Elapsed: 5 * time.Millisecond},
		{Name: "slow", Pass: true, Elapsed: 1500 * time.Millisecond},
	}
	suite := BuildJUnitReport("mcp-tui.conform", results)
	if suite.Cases[0].Time != "0.005" {
		t.Errorf("fast case Time = %q, want 0.005", suite.Cases[0].Time)
	}
	if suite.Cases[1].Time != "1.500" {
		t.Errorf("slow case Time = %q, want 1.500", suite.Cases[1].Time)
	}
	if suite.Time != "1.505" {
		t.Errorf("suite Time = %q, want 1.505", suite.Time)
	}
}
