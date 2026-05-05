// Package conform implements the end-to-end MCP conformance suite. The
// `mcp-tui conform <url|cmd>` subcommand runs a curated battery of MCP
// protocol scenarios plus every verify probe, prints a per-scenario PASS/
// FAIL summary, and (when `--report-junit <file>` is set) writes a JUnit
// XML report consumable by CI dashboards.
//
// Scenario authors implement the Scenario interface; the dispatcher in
// scenarios.go resolves a name to a typed Scenario value and runs it
// against the supplied Target. JUnit XML marshaling lives in junit.go.
package conform

import (
	"encoding/xml"
	"fmt"
	"io"
	"time"
)

// JUnitTestSuite is the top-level <testsuite> element of a JUnit-XML report.
//
// We follow the de-facto Maven Surefire schema (the most widely consumed
// shape): a testsuite element with attributes for counts and elapsed time,
// containing testcase elements. Each testcase has classname (the suite),
// name (the scenario), and time. A failing testcase contains a <failure>
// element with the failure message and stack/text body.
//
// The struct tags use `xml:"...,attr"` for attributes and `xml:",chardata"`
// for element bodies. Field order in the struct dictates emission order
// because encoding/xml marshals fields top-to-bottom.
type JUnitTestSuite struct {
	XMLName  xml.Name        `xml:"testsuite"`
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Errors   int             `xml:"errors,attr"`
	Time     string          `xml:"time,attr"` // seconds, fixed-point — Surefire format
	Cases    []JUnitTestCase `xml:"testcase"`
}

// JUnitTestCase represents a single <testcase> element. ClassName is the
// suite name (we use "mcp-tui.conform"), Name is the scenario identifier.
// Time is elapsed-seconds in fixed-point form. Failure is non-nil when the
// scenario failed; SystemOut carries any captured diagnostic output (skipped
// for now to keep payloads compact).
type JUnitTestCase struct {
	XMLName   xml.Name      `xml:"testcase"`
	ClassName string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
}

// JUnitFailure holds the failure metadata. Message is the short summary
// shown in CI dashboards; Type categorises the failure ("Failure" vs
// "Error" — we always emit "Failure" because conformance is binary). The
// chardata body carries the detail (stack-trace equivalent).
type JUnitFailure struct {
	XMLName xml.Name `xml:"failure"`
	Message string   `xml:"message,attr"`
	Type    string   `xml:"type,attr"`
	Body    string   `xml:",chardata"`
}

// BuildJUnitReport converts the per-scenario ScenarioResult slice into a
// JUnit-shaped testsuite. The conversion is pure: same input always
// produces the same output, scenarios are emitted in the slice's order so
// the conform-run report mirrors the runner's iteration order.
//
// suiteName populates the testsuite name attribute and is also used as the
// classname on every testcase so CI dashboards group them under one suite.
// Pass "mcp-tui.conform" for the canonical run.
func BuildJUnitReport(suiteName string, results []ScenarioResult) JUnitTestSuite {
	suite := JUnitTestSuite{
		Name:  suiteName,
		Cases: make([]JUnitTestCase, 0, len(results)),
	}
	var totalElapsed time.Duration
	for _, r := range results {
		totalElapsed += r.Elapsed
		tc := JUnitTestCase{
			ClassName: suiteName,
			Name:      r.Name,
			Time:      formatJUnitDuration(r.Elapsed),
		}
		if !r.Pass {
			suite.Failures++
			tc.Failure = &JUnitFailure{
				Message: r.Error,
				Type:    "Failure",
				Body:    r.Detail,
			}
		}
		suite.Cases = append(suite.Cases, tc)
	}
	suite.Tests = len(results)
	suite.Time = formatJUnitDuration(totalElapsed)
	return suite
}

// formatJUnitDuration renders a duration in fixed-point seconds with three
// decimal places — the format Surefire/Jenkins/CircleCI all parse cleanly.
// We deliberately avoid time.Duration.String() ("1.234s") because some CI
// parsers reject the trailing "s".
func formatJUnitDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%.3f", d.Seconds())
}

// WriteJUnitReport marshals suite to w as indented JUnit XML with the
// canonical XML 1.0 prolog. Returns an error if the underlying writer
// fails or marshaling fails (which only happens when the struct contains
// an un-marshalable field, never in our case — but we surface it anyway
// so callers don't have to wrap).
//
// The output is deterministic for a given suite value: encoding/xml
// preserves struct field order and slice element order.
func WriteJUnitReport(w io.Writer, suite JUnitTestSuite) error {
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return fmt.Errorf("write XML prolog: %w", err)
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(suite); err != nil {
		return fmt.Errorf("encode JUnit XML: %w", err)
	}
	if err := enc.Flush(); err != nil {
		return fmt.Errorf("flush JUnit XML: %w", err)
	}
	// Trailing newline so editors don't show "no newline at end of file".
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("write trailing newline: %w", err)
	}
	return nil
}
