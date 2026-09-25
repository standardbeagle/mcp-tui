package screens

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/aymanbagabas/go-osc52/v2"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	"github.com/standardbeagle/mcp-tui/internal/mcp/inputschema"
	"github.com/standardbeagle/mcp-tui/internal/mcp/tasks"
	"github.com/standardbeagle/mcp-tui/internal/shell"
	"github.com/standardbeagle/mcp-tui/internal/tui/components"
)

// Layout constants for result scrolling
const (
	// resultReservedHeightBase is the base height reserved for UI elements
	// (title, description, buttons, execution header, help, status)
	resultReservedHeightBase = 15

	// resultHeightPerField is the height consumed by each form field
	resultHeightPerField = 3

	// resultMinHeight is the minimum height for the result display area
	resultMinHeight = 5

	// defaultTermWidth is the fallback terminal width if not detected
	defaultTermWidth = 80

	// defaultTermHeight is the fallback terminal height if not detected
	defaultTermHeight = 30

	// resultWidthMargin is the margin subtracted from terminal width for result display
	resultWidthMargin = 6
)

// ToolScreen allows interactive tool execution
type ToolScreen struct {
	*BaseScreen
	logger debug.Logger

	// clipboard is the OS clipboard boundary; tests swap in an in-memory one.
	clipboard clipboardReadWriter

	// Tool info
	tool       mcp.Tool
	mcpService mcp.Service

	// Form fields
	fields []toolField
	cursor int // current field index

	// Raw JSON mode (when schema parsing fails)
	rawJSONMode  bool            // Whether we're in raw JSON input mode
	rawJSONInput textinput.Model // Input for raw JSON arguments

	// Execution state
	executing      bool
	executionStart time.Time
	executionCount int       // Number of times the tool has been executed
	lastExecution  time.Time // Time of last execution
	result         *mcp.CallToolResult
	resultJSON     string // Pretty-printed JSON result
	// callProgress is the server's progress on the running call.
	callProgress callProgress

	// CLI command state
	cliCommand     string // Generated CLI command
	showCLICommand bool   // Whether to show the CLI command

	// pendingConfirm tracks an outstanding destructive-tool confirm overlay so
	// the user's Y/N decision (delivered via ConfirmDecisionMsg) can resume
	// execution. It is set when the user presses Enter on Execute for a tool
	// where IsDestructive() returns true, and cleared when the decision
	// arrives. While set, executeTool runs without re-prompting.
	pendingConfirm bool

	// confirmBypassed records that the user already approved the most recent
	// confirm prompt for this run; the executeTool path checks it once and
	// clears it so a subsequent independent execution still triggers a fresh
	// prompt.
	confirmBypassed bool

	// MCP task mode (Ctrl+T): Execute calls the tool as a task and follows
	// it; runningTask is the task being followed, taskUpdates its status
	// changes and, last, its result.
	taskMode    bool
	runningTask *tasks.Task
	taskUpdates <-chan tea.Msg

	// inputSchema is the parsed input schema; the arguments are validated
	// against it before a call. Zero (accepting anything) when it did not
	// parse.
	inputSchema inputschema.Schema
	// skipArgValidation (Ctrl+O) sends a call whose arguments break the
	// input schema instead of refusing it; argumentViolation is what the
	// last call sent broke, "" when it broke nothing.
	skipArgValidation bool
	argumentViolation string
	// schemaNote says why the input schema's root is not shown as a form
	// (the raw JSON editor is used instead).
	schemaNote string

	// Result viewing mode
	viewingResult bool          // Whether we're in result viewing mode
	resultFields  []resultField // Parsed JSON fields
	resultCursor  int           // Current field in result view

	// Result scrolling
	resultScroll    int      // Scroll offset for result display
	resultLineCount int      // Total lines in result
	resultLines     []string // Cached lines from result JSON

	// Styles
	titleStyle          lipgloss.Style
	labelStyle          lipgloss.Style
	inputStyle          lipgloss.Style
	selectedStyle       lipgloss.Style
	buttonStyle         lipgloss.Style
	selectedButtonStyle lipgloss.Style
	resultStyle         lipgloss.Style
	errorStyle          lipgloss.Style
	warningStyle        lipgloss.Style
	helpStyle           lipgloss.Style
}

// toolField represents a single input field
type toolField struct {
	name        string
	description string
	// fieldType is the JSON type the value takes (inputschema.Param.Kind).
	fieldType inputschema.Kind
	// nullable: the literal "null" sends null (for non-string types), and
	// Ctrl+N sets sendNull, which sends null whatever the input holds.
	nullable bool
	sendNull bool
	// itemKind is the type of an array's items; comma-separated input is
	// only accepted for string (or unknown) items.
	itemKind inputschema.Kind
	// union lists a KindUnion field's types; inferredKind is the one the
	// current value's syntax picks.
	union        []inputschema.Kind
	inferredKind inputschema.Kind
	// note says what the schema could not express for this field.
	note            string
	required        bool
	input           textinput.Model
	validationError string // Real-time validation error

	// depth is how many sub-forms down the field sits; a sub-form's fields
	// follow the field that opened it, one level deeper.
	depth int
	// properties are an object's own properties, itemProperties those of an
	// array's object items; Ctrl+E opens them as a sub-form (expanded),
	// whose fields are kept in stash while closed. An array's sub-form is a
	// list of element rows (element), each followed by its object's fields.
	properties     []inputschema.Param
	itemProperties []inputschema.Param
	element        bool
	expanded       bool
	stash          []toolField
}

// hasSubForm reports whether Ctrl+E opens the field as a sub-form.
func (f *toolField) hasSubForm() bool {
	return !f.element && (len(f.properties) > 0 || len(f.itemProperties) > 0)
}

// isElementList reports an array of objects open as a list of elements.
func (f *toolField) isElementList() bool {
	return f.expanded && len(f.itemProperties) > 0
}

// resultField represents a parsed field from JSON result
type resultField struct {
	path  string      // JSON path like "data.id" or "items[0].name"
	value string      // String representation of the value
	raw   interface{} // Raw value
}

// NewToolScreen creates a new tool execution screen
func NewToolScreen(tool *mcp.Tool, service mcp.Service) *ToolScreen {
	ts := &ToolScreen{
		BaseScreen: NewBaseScreen("Tool", true),
		logger:     debug.Component("tool-screen"),
		clipboard:  systemClipboard{},
		tool:       *tool,
		mcpService: service,
	}

	// Initialize styles
	ts.initStyles()

	// Parse tool schema to create fields
	ts.parseSchema()

	return ts
}

// getResultDisplayHeight calculates the available height for result display.
// Used by scroll handlers; View() recomputes the same value dynamically with
// actual header/footer measurements when the result is present.
func (ts *ToolScreen) getResultDisplayHeight() int {
	termHeight := ts.Height()
	if termHeight == 0 {
		termHeight = defaultTermHeight
	}

	reservedHeight := resultReservedHeightBase + len(ts.fields)*resultHeightPerField
	availableHeight := termHeight - reservedHeight
	if availableHeight < resultMinHeight {
		availableHeight = resultMinHeight
	}
	return availableHeight
}

// resultChromeHeight is overhead inside the result block: leading blank,
// execution info line, "Result:" label, border (2), trailing blank/scroll/hint.
const resultChromeHeight = 7

// computeResultDisplayHeight derives result body height from actual rendered
// header/footer heights so the panel fills available screen space.
func (ts *ToolScreen) computeResultDisplayHeight(headerH, footerH int) int {
	termHeight := ts.Height()
	if termHeight == 0 {
		termHeight = defaultTermHeight
	}
	avail := termHeight - headerH - footerH - resultChromeHeight
	if avail < resultMinHeight {
		avail = resultMinHeight
	}
	return avail
}

// clipboardReadWriter is the clipboard boundary ToolScreen reads and writes
// through, so tests never shell out to xclip/xsel (which block under WSLg).
type clipboardReadWriter interface {
	ReadAll() (string, error)
	WriteAll(text string) error
}

// systemClipboard is the production clipboardReadWriter backed by the OS.
type systemClipboard struct{}

func (systemClipboard) ReadAll() (string, error)   { return clipboard.ReadAll() }
func (systemClipboard) WriteAll(text string) error { return clipboard.WriteAll(text) }

// copyToClipboard copies text to the system clipboard, falling back to an
// OSC52 terminal escape. Both failures are reported: silently returning nil
// makes callers announce a successful copy that never happened.
func (ts *ToolScreen) copyToClipboard(text string) error {
	clipErr := ts.clipboard.WriteAll(text)
	if clipErr == nil {
		return nil
	}

	// Fall back to OSC52 for terminal clipboard
	if _, err := fmt.Fprint(os.Stderr, osc52.New(text)); err != nil {
		return fmt.Errorf("clipboard unavailable (%v) and OSC52 write failed: %w", clipErr, err)
	}
	return nil
}

// readFromClipboard reads text from clipboard using multiple methods
func (ts *ToolScreen) readFromClipboard() (string, error) {
	// Try standard clipboard first
	if text, err := ts.clipboard.ReadAll(); err == nil && text != "" {
		return text, nil
	}

	// OSC52 doesn't support reading, so we return an error
	return "", fmt.Errorf("clipboard read not available - try using Ctrl+Shift+V or right-click paste")
}

// sanitizeInput removes control characters and ANSI escape sequences that could corrupt the display
func (ts *ToolScreen) sanitizeInput(input string) string {
	// Remove ANSI escape sequences
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	cleaned := ansiRegex.ReplaceAllString(input, "")

	// Remove other control characters except newlines and tabs
	var result strings.Builder
	for _, r := range cleaned {
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// generateCLICommand generates the equivalent CLI command for the current
// tool call, as a POSIX shell (sh, bash, zsh) command line.
func (ts *ToolScreen) generateCLICommand() string {
	var builder strings.Builder

	// Start with the base command
	builder.WriteString("mcp-tui")

	// Add porcelain flag for clean output (suitable for scripting)
	builder.WriteString(" --porcelain")

	// Get connection configuration from service
	connConfig := ts.mcpService.GetConnectionConfig()
	if connConfig == nil {
		// Fallback if no connection config available
		return "# Connection config not available - cannot generate CLI command"
	}

	// Every word goes through shell.Quote, so the shell passes each value
	// on as typed: nothing in it is expanded or split.
	fmt.Fprintf(&builder, " --transport %s", shell.Quote(string(connConfig.Type)))
	if connConfig.Command != "" {
		fmt.Fprintf(&builder, " --cmd %s", shell.Quote(connConfig.Command))
	}
	// One --arg per server argument: --args would split one holding a comma.
	for _, arg := range connConfig.Args {
		fmt.Fprintf(&builder, " --arg %s", shell.Quote(arg))
	}
	if connConfig.URL != "" {
		fmt.Fprintf(&builder, " --url %s", shell.Quote(connConfig.URL))
	}

	// Add the tool command
	fmt.Fprintf(&builder, " tool call %s", shell.Quote(ts.tool.Name))
	if ts.skipArgValidation {
		builder.WriteString(" --skip-arg-validation")
	}

	if ts.rawJSONMode {
		words, err := ts.rawJSONArgumentWords()
		if err != nil {
			return "# Cannot copy the raw JSON arguments as a command: " + err.Error()
		}
		for _, word := range words {
			builder.WriteString(" " + shell.Quote(word))
		}
		return builder.String()
	}

	// Add arguments from form fields; an object filled in as a sub-form is
	// written as the JSON the form builds for it.
	// A form that does not convert yet (a half-typed field) has no JSON
	// for its sub-forms, which are then left out.
	built, buildErr := ts.formArguments()
	for i := range ts.fields {
		field := &ts.fields[i]
		if field.depth > 0 {
			continue
		}
		switch {
		case field.expanded:
			if v, ok := built[field.name]; ok && buildErr == nil {
				if encoded, err := json.Marshal(v); err == nil {
					builder.WriteString(" " + shell.Quote(field.name+"="+string(encoded)))
				}
			}
		case field.sendNull:
			builder.WriteString(" " + shell.Quote(field.name+":=null"))
		case field.input.Value() != "":
			// key=value reads the value by the field's schema type, as
			// the form does.
			builder.WriteString(" " + shell.Quote(field.name+"="+field.input.Value()))
		}
	}

	return builder.String()
}

// initStyles initializes the visual styles
func (ts *ToolScreen) initStyles() {
	ts.titleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("13")).
		Bold(true).
		Margin(1, 0)

	ts.labelStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("7"))

	ts.inputStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("4")).
		Padding(0, 1).
		Width(60)

	ts.selectedStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(0, 1).
		Width(60)

	ts.buttonStyle = lipgloss.NewStyle().
		Padding(0, 2).
		Background(lipgloss.Color("8")).
		Foreground(lipgloss.Color("0"))

	ts.selectedButtonStyle = lipgloss.NewStyle().
		Padding(0, 2).
		Background(lipgloss.Color("6")).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	ts.resultStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1).
		Width(80)

	ts.errorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("9")).
		Bold(true)

	ts.warningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("11"))

	ts.helpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
}

// parseSchema converts the tool's input schema into form fields
func (ts *ToolScreen) parseSchema() {
	ts.fields = []toolField{}
	ts.rawJSONMode = false

	// A schema that does not resolve (remote $ref, dangling local $ref,
	// invalid keyword), or whose root the form cannot express, falls back
	// to raw JSON with the reason shown in a banner, rather than a form
	// missing the parameter.
	if !ts.tool.HasSchemaError() {
		schema, err := inputschema.Parse(ts.tool.Name, ts.tool.InputSchema)
		ts.inputSchema = schema
		switch {
		case err != nil:
			ts.tool.SchemaError = &mcp.SchemaError{Message: err.Error()}
		case schema.Note != "":
			// A form built from part of the root would mislead.
			ts.schemaNote = schema.Note
		default:
			ts.fields = fieldsFromSchema(&schema)
			return
		}
	}

	ts.rawJSONMode = true
	ts.rawJSONInput = textinput.New()
	ts.rawJSONInput.Placeholder = `{"key": "value"}`
	ts.rawJSONInput.CharLimit = 0
	ts.rawJSONInput.Width = 60
}

// fieldsFromSchema builds one text input per schema parameter, in name
// order.
func fieldsFromSchema(schema *inputschema.Schema) []toolField {
	return fieldsFromParams(schema.Params, 0)
}

// fieldsFromParams builds one text input per parameter of an object depth
// levels down.
func fieldsFromParams(params []inputschema.Param, depth int) []toolField {
	fields := make([]toolField, 0, len(params))
	for i := range params {
		p := &params[i]
		input := textinput.New()
		input.CharLimit = 0 // No limit
		input.Width = 58    // Slightly smaller than the border width
		switch p.Kind {
		case inputschema.KindNumber:
			input.Placeholder = "Enter a number"
		case inputschema.KindInteger:
			input.Placeholder = "Enter an integer"
		case inputschema.KindBoolean:
			input.Placeholder = "true or false"
		case inputschema.KindArray:
			if commaSeparatedItems(p.ItemKind) {
				input.Placeholder = "JSON array or comma-separated"
			} else {
				input.Placeholder = "JSON array"
			}
		case inputschema.KindObject:
			input.Placeholder = "JSON object"
		case inputschema.KindUnion:
			input.Placeholder = p.UnionLabel() + " (type read from the value)"
		case inputschema.KindJSON:
			input.Placeholder = "JSON value (or plain text)"
		default:
			input.Placeholder = "Enter " + p.Name
		}
		fields = append(fields, toolField{
			name:           p.Name,
			description:    p.Description,
			fieldType:      p.Kind,
			nullable:       p.Nullable,
			itemKind:       p.ItemKind,
			union:          p.Union,
			note:           p.Note,
			required:       p.Required,
			input:          input,
			depth:          depth,
			properties:     p.Properties,
			itemProperties: p.ItemProperties,
		})
	}
	return fields
}

// keyToggleSubForm opens and closes an object field's sub-form, or an array
// of objects' list of elements.
const keyToggleSubForm = "ctrl+e"

// keyAddElement and keyRemoveElement add an element to the array of objects
// the cursor is on or in, and remove the element the cursor is in. They
// take over the text input's own Ctrl+A (line start; Home still does it)
// only there.
const (
	keyAddElement    = "ctrl+a"
	keyRemoveElement = "ctrl+x"
)

// keyToggleArgValidation switches between refusing a call whose arguments
// break the input schema and sending it with the violation shown.
const keyToggleArgValidation = "ctrl+o"

// argValidationOffBadge marks the title while calls that break the input
// schema are sent.
const argValidationOffBadge = "[schema violations sent]"

// toggleSubForm opens the object field at index as a sub-form of its
// properties, inserted below it, or closes an open one, keeping its fields
// (and what was typed in them) for the next opening. It reports whether
// the field has a sub-form.
func (ts *ToolScreen) toggleSubForm(index int) bool {
	field := &ts.fields[index]
	if !field.hasSubForm() || field.sendNull {
		return false
	}
	field.input.Blur()
	if field.expanded {
		end := index + 1
		for end < len(ts.fields) && ts.fields[end].depth > field.depth {
			end++
		}
		field.stash = slices.Clone(ts.fields[index+1 : end])
		field.expanded = false
		ts.fields = slices.Delete(ts.fields, index+1, end)
		ts.cursor = index
		ts.fields[index].input.Focus()
		return true
	}
	children := field.stash
	if children == nil && len(field.itemProperties) > 0 {
		children = elementFields(field, 0)
	} else if children == nil {
		children = fieldsFromParams(field.properties, field.depth+1)
	}
	field.stash = nil
	field.expanded = true
	field.validationError = ""
	ts.fields = slices.Insert(ts.fields, index+1, children...)
	ts.focusFirstInput(index+1, index+1+len(children), index)
	return true
}

// subFormEnd is the index just past the fields of the sub-form (or element)
// opened by fields[index].
func (ts *ToolScreen) subFormEnd(index int) int {
	end := index + 1
	for end < len(ts.fields) && ts.fields[end].depth > ts.fields[index].depth {
		end++
	}
	return end
}

// focusFirstInput moves the cursor to the first field in fields[from:end]
// that takes typing (element rows do not), or to fallback when none does.
func (ts *ToolScreen) focusFirstInput(from, end, fallback int) {
	ts.cursor = fallback
	for i := from; i < end; i++ {
		if !ts.fields[i].element {
			ts.cursor = i
			break
		}
	}
	ts.fields[ts.cursor].input.Focus()
}

// elementFields is element n of the array of objects list: its row and its
// object's fields.
func elementFields(list *toolField, n int) []toolField {
	row := toolField{
		name:       fmt.Sprintf("[%d]", n),
		fieldType:  inputschema.KindObject,
		depth:      list.depth + 1,
		properties: list.itemProperties,
		element:    true,
		expanded:   true,
	}
	return append([]toolField{row}, fieldsFromParams(list.itemProperties, list.depth+2)...)
}

// enclosingElementList finds the array of objects list the field at index
// is on or inside: list is the array field, element the row of the element
// holding index (-1 when index is the array field itself). list is -1 when
// there is none.
func (ts *ToolScreen) enclosingElementList(index int) (list, element int) {
	if f := &ts.fields[index]; len(f.itemProperties) > 0 {
		return index, -1
	}
	depth := ts.fields[index].depth
	for i := index; i >= 0; i-- {
		f := &ts.fields[i]
		if i != index && f.depth >= depth {
			continue
		}
		depth = f.depth
		if !f.element {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if ts.fields[j].depth < f.depth {
				return j, i
			}
		}
	}
	return -1, -1
}

// addElement appends an element to the array of objects the cursor is on or
// in, opening its list first when closed, and moves the cursor into it. It
// reports whether the cursor is on or in such an array.
func (ts *ToolScreen) addElement() bool {
	list, _ := ts.enclosingElementList(ts.cursor)
	if list < 0 || ts.fields[list].sendNull {
		return false
	}
	ts.fields[ts.cursor].input.Blur()
	if !ts.fields[list].expanded {
		return ts.toggleSubForm(list)
	}
	end := ts.subFormEnd(list)
	elements := 0
	for i := list + 1; i < end; i++ {
		if ts.fields[i].depth == ts.fields[list].depth+1 {
			elements++
		}
	}
	added := elementFields(&ts.fields[list], elements)
	ts.fields = slices.Insert(ts.fields, end, added...)
	ts.focusFirstInput(end, end+len(added), list)
	return true
}

// removeElement removes the element the cursor is in and numbers the rest
// again. It reports whether the cursor is in an element.
func (ts *ToolScreen) removeElement() bool {
	list, element := ts.enclosingElementList(ts.cursor)
	if element < 0 {
		return false
	}
	ts.fields[ts.cursor].input.Blur()
	ts.fields = slices.Delete(ts.fields, element, ts.subFormEnd(element))
	n := 0
	for i := list + 1; i < ts.subFormEnd(list); i++ {
		if ts.fields[i].element && ts.fields[i].depth == ts.fields[list].depth+1 {
			ts.fields[i].name = fmt.Sprintf("[%d]", n)
			n++
		}
	}
	ts.focusFirstInput(element, ts.subFormEnd(list), list)
	return true
}

// commaSeparatedItems reports whether an array whose items are of kind may
// be typed as "a, b, c": splitting yields strings, so only string (or
// undeclared) items qualify.
func commaSeparatedItems(kind inputschema.Kind) bool {
	return kind == "" || kind == inputschema.KindString
}

// typeIntoField hands msg to the focused field's text input and validates
// what it holds. A field sent as null takes no typing until Ctrl+N again,
// nor does an object shown as a sub-form.
func (ts *ToolScreen) typeIntoField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	field := &ts.fields[ts.cursor]
	if field.sendNull || field.expanded {
		return ts, nil
	}
	var cmd tea.Cmd
	field.input, cmd = field.input.Update(msg)
	ts.validateField(ts.cursor)
	return ts, cmd
}

// Init initializes the tool screen
func (ts *ToolScreen) Init() tea.Cmd {
	ts.logger.Info("Initializing tool screen", debug.F("tool", ts.tool.Name))

	// Focus the appropriate input
	if ts.rawJSONMode {
		ts.rawJSONInput.Focus()
	} else if len(ts.fields) > 0 {
		ts.fields[0].input.Focus()
	}

	return nil
}

// Update handles messages for the tool screen
func (ts *ToolScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		ts.UpdateSize(msg.Width, msg.Height)
		return ts, nil

	case tea.KeyMsg:
		return ts.handleKeyMsg(msg)

	case toolTaskStartedMsg:
		ts.executing = false
		ts.runningTask, ts.taskUpdates = &msg.task, msg.updates
		ts.SetStatus(fmt.Sprintf("Task %s started (%s); following it — T on the main screen lists tasks",
			msg.task.ID, msg.task.Status), StatusInfo)
		return ts, waitForTaskUpdate(msg.updates)

	case toolTaskProgressMsg:
		ts.runningTask = &msg.task
		ts.SetStatus(taskProgressLine(&msg.task), StatusInfo)
		return ts, waitForTaskUpdate(ts.taskUpdates)

	case toolExecutionCompleteMsg:
		ts.runningTask, ts.taskUpdates = nil, nil
		ts.callProgress.clear()
		ts.executing = false
		ts.lastExecution = time.Now()
		ts.executionCount++
		// Reset bypass so a subsequent execution triggers a fresh confirm.
		ts.confirmBypassed = false

		if msg.Error != nil {
			ts.SetError(msg.Error)
		} else {
			ts.result = msg.Result
			// Reset scroll state when new result arrives
			ts.resultScroll = 0
			ts.resultLines = nil
			ts.resultLineCount = 0

			// Pretty print JSON result
			if len(msg.Result.Content) > 0 {
				// For now, just handle text content
				var resultText strings.Builder
				for i, content := range msg.Result.Content {
					if i > 0 {
						resultText.WriteString("\n\n")
					}
					if content.Type == "text" {
						text := content.Text
						// Try to pretty-print JSON
						var jsonData interface{}
						if err := json.Unmarshal([]byte(text), &jsonData); err == nil {
							if formatted, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
								resultText.Write(formatted)
							} else {
								resultText.WriteString(text)
							}
						} else {
							resultText.WriteString(text)
						}
					} else {
						if jsonBytes, err := json.MarshalIndent(content, "", "  "); err == nil {
							resultText.Write(jsonBytes)
						} else {
							fmt.Fprintf(&resultText, "%v", content)
						}
					}
				}
				ts.resultJSON = resultText.String()

				// Cache lines for scrolling (compute once, use in View)
				ts.resultLines = strings.Split(ts.resultJSON, "\n")
				ts.resultLineCount = len(ts.resultLines)

				// Parse result fields for viewing
				ts.parseResultFields()
			}

			// Show execution count in status
			// Tool-result errors (isError:true) are NOT JSON-RPC failures —
			// the call completed and the server returned a structured result
			// flagged as an error. Surface that distinction in the status bar
			// so operators see "Tool reported an error" rather than the
			// misleading "executed successfully" message that older builds
			// printed for every non-protocol-error path.
			if msg.Result != nil && msg.Result.IsError {
				errMsg := fmt.Sprintf("Tool reported an error (isError:true) (#%d)", ts.executionCount)
				ts.SetStatus(errMsg, StatusError)
			} else {
				execMsg := fmt.Sprintf("Tool executed successfully (#%d)", ts.executionCount)
				if ts.executionCount > 1 {
					execMsg = fmt.Sprintf("Tool executed successfully (#%d) ✨", ts.executionCount)
				}
				ts.SetStatus(execMsg, StatusSuccess)
			}
		}
		return ts, nil

	case StatusMsg:
		ts.SetStatus(msg.Message, msg.Level)
		return ts, nil

	case toolSpinnerTickMsg:
		// Keep redrawing while the call, or the task it started, runs: the
		// progress line changes between ticks.
		if ts.executing || ts.runningTask != nil {
			return ts, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
				return toolSpinnerTickMsg{}
			})
		}
		return ts, nil

	case ConfirmDecisionMsg:
		// Confirm overlay finished. The ToolName check guards against stale
		// decisions arriving after the user navigated to a different tool,
		// which is unlikely with the current screen flow but cheap to defend.
		if msg.ToolName != ts.tool.Name {
			return ts, nil
		}
		ts.pendingConfirm = false
		if msg.Approved {
			ts.confirmBypassed = true
			ts.SetStatus("Confirmed — executing destructive tool", StatusWarning)
			cmd := ts.executeTool()
			return ts, cmd
		}
		ts.SetStatus("Execution cancelled by user", StatusInfo)
		return ts, nil
	}

	return ts, nil
}

// toolExecutionCompleteMsg signals tool execution is complete
type toolExecutionCompleteMsg struct {
	Result *mcp.CallToolResult
	Error  error
}

// toolSpinnerTickMsg is sent to update the spinner animation
type toolSpinnerTickMsg struct{}

// toolTaskStartedMsg: a task-mode call created a task; updates delivers
// its status changes and then a toolExecutionCompleteMsg.
type toolTaskStartedMsg struct {
	task    tasks.Task
	updates <-chan tea.Msg
}

// toolTaskProgressMsg is a status change of the task being followed.
type toolTaskProgressMsg struct {
	task tasks.Task
}

// BackgroundWork marks the tool screen's reports as BackgroundMsg, so an
// overlay (an elicitation the call raised, the debug view) cannot swallow
// them.
func (toolExecutionCompleteMsg) BackgroundWork() {}
func (toolSpinnerTickMsg) BackgroundWork()       {}
func (toolTaskStartedMsg) BackgroundWork()       {}
func (toolTaskProgressMsg) BackgroundWork()      {}

// taskUpdateBuffer holds the status changes a slow screen has not read
// yet; the last slot is kept for the result, so sending it never blocks.
const taskUpdateBuffer = 32

func waitForTaskUpdate(updates <-chan tea.Msg) tea.Cmd {
	if updates == nil {
		return nil
	}
	return func() tea.Msg { return <-updates }
}

func taskProgressLine(t *tasks.Task) string {
	line := fmt.Sprintf("Task %s: %s", t.ID, t.Status)
	if t.StatusMessage != "" {
		line += " — " + t.StatusMessage
	}
	return line
}

// toggleTaskMode switches task mode, refusing when the server did not
// declare tasks.
func (ts *ToolScreen) toggleTaskMode() {
	if ts.taskMode {
		ts.taskMode = false
		ts.SetStatus("Task mode off: Execute calls the tool directly", StatusInfo)
		return
	}
	if ts.mcpService == nil || !ts.mcpService.TaskSupport().Declared {
		ts.SetStatus("The server did not declare MCP tasks; task mode is unavailable", StatusWarning)
		return
	}
	ts.taskMode = true
	ts.SetStatus("Task mode on: Execute runs the tool as an MCP task", StatusInfo)
}

// toggleArgValidation switches between refusing a call whose arguments
// break the input schema (the default) and sending it with the violation
// shown: a test client needs to see how a server rejects a bad call.
func (ts *ToolScreen) toggleArgValidation() {
	ts.skipArgValidation = !ts.skipArgValidation
	if ts.skipArgValidation {
		ts.SetStatus("Arguments that break the input schema are sent, with the violation shown", StatusWarning)
		return
	}
	ts.SetStatus("Arguments that break the input schema are refused", StatusInfo)
}

// startTaskCmd calls the tool as a task. A created task is followed in the
// background until it ends; a direct answer completes at once.
func (ts *ToolScreen) startTaskCmd(args map[string]interface{}) tea.Cmd {
	svc, name := ts.mcpService, ts.tool.Name
	// 2025-11-25 tasks keep reporting progress on the call's token while
	// AwaitTask follows them.
	observe := ts.callProgress.start()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(mcp.WithProgressObserver(context.Background(), observe), 30*time.Second)
		defer cancel()
		outcome, err := svc.CallToolAsTask(ctx, mcp.CallToolRequest{Name: name, Arguments: args}, nil)
		if err != nil {
			return toolExecutionCompleteMsg{Error: err}
		}
		if outcome.Task == nil {
			return toolExecutionCompleteMsg{Result: outcome.Result}
		}
		updates := make(chan tea.Msg, taskUpdateBuffer)
		id := outcome.Task.ID
		go func() {
			result, err := svc.AwaitTask(mcp.WithProgressObserver(context.Background(), observe), id, func(t tasks.Task) {
				if len(updates) < taskUpdateBuffer-1 {
					updates <- toolTaskProgressMsg{task: t}
				}
			})
			updates <- toolExecutionCompleteMsg{Result: result, Error: err}
		}()
		return toolTaskStartedMsg{task: *outcome.Task, updates: updates}
	}
}

// handleKeyMsg handles keyboard input
func (ts *ToolScreen) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Don't process keys while executing
	if ts.executing {
		if msg.String() == keyCtrlC {
			// Allow canceling during execution
			return ts, func() tea.Msg { return BackMsg{} }
		}
		return ts, nil
	}

	// Handle raw JSON mode input
	if ts.rawJSONMode && ts.cursor == 0 {
		switch msg.String() {
		case keyTab, keyDown:
			// Move to execute button
			ts.rawJSONInput.Blur()
			ts.cursor = 1
			return ts, nil
		case keyEnter:
			// If on input, move to button; if on button, execute
			if ts.cursor == 0 {
				ts.rawJSONInput.Blur()
				ts.cursor = 1
				return ts, nil
			}
		case keyEsc:
			return ts, func() tea.Msg { return BackMsg{} }
		case keyToggleArgValidation:
			ts.toggleArgValidation()
			return ts, nil
		default:
			// Pass to raw JSON input
			var cmd tea.Cmd
			ts.rawJSONInput, cmd = ts.rawJSONInput.Update(msg)
			return ts, cmd
		}
	}

	// If we're in an input field, let the textinput handle most keys first
	if !ts.rawJSONMode && ts.cursor < len(ts.fields) {
		field := &ts.fields[ts.cursor]

		// Handle navigation keys before passing to textinput
		switch msg.String() {
		case keyTab, keyDown, keyEnter:
			// Don't pass these to textinput, handle navigation
		case keyShiftTab, keyUp:
			// Don't pass these to textinput, handle navigation
		case "ctrl+t":
			// Task mode toggle, handled below
		case keyToggleArgValidation:
			ts.toggleArgValidation()
			return ts, nil
		case keyToggleSubForm:
			if ts.toggleSubForm(ts.cursor) {
				ts.SetStatus("", StatusInfo)
			}
			return ts, nil
		case keyAddElement:
			if ts.addElement() {
				ts.SetStatus("", StatusInfo)
				return ts, nil
			}
			return ts.typeIntoField(msg)
		case keyRemoveElement:
			if ts.removeElement() {
				ts.SetStatus("", StatusInfo)
				return ts, nil
			}
			return ts.typeIntoField(msg)
		case "ctrl+n":
			// Send null: the only way to say it for a nullable string. An
			// open sub-form says what the object holds instead.
			if field.nullable && !field.expanded {
				field.sendNull = !field.sendNull
				field.validationError = ""
				if field.sendNull {
					ts.SetStatus(fmt.Sprintf("'%s' will be sent as null", field.name), StatusInfo)
				} else {
					ts.validateField(ts.cursor)
					ts.SetStatus(fmt.Sprintf("'%s' takes its typed value again", field.name), StatusInfo)
				}
			}
			return ts, nil
		case keyEsc:
			// Don't pass to textinput, handle escape
		case "ctrl+v":
			// Paste clipboard contents into the focused field. The help text
			// advertises this binding; textinput itself ignores ctrl+v.
			text, err := ts.readFromClipboard()
			if err != nil {
				ts.SetStatus(err.Error(), StatusError)
				return ts, nil
			}
			field.input.SetValue(ts.sanitizeInput(text))
			ts.validateField(ts.cursor)
			ts.SetStatus("Pasted from clipboard", StatusSuccess)
			return ts, nil
		default:
			return ts.typeIntoField(msg)
		}
	}

	// Special handling for result scrolling (when not in viewing mode)
	if ts.result != nil && !ts.viewingResult {
		availableHeight := ts.getResultDisplayHeight()

		switch msg.String() {
		case "ctrl+up":
			// Scroll result up
			if ts.resultScroll > 0 {
				ts.resultScroll--
			}
			return ts, nil

		case "ctrl+down":
			// Scroll result down
			maxScroll := max(0, ts.resultLineCount-availableHeight)
			if ts.resultScroll < maxScroll {
				ts.resultScroll++
			}
			return ts, nil

		case keyPgUp:
			// Page up in result
			pageSize := max(1, availableHeight-2)
			ts.resultScroll -= pageSize
			if ts.resultScroll < 0 {
				ts.resultScroll = 0
			}
			return ts, nil

		case keyPgDown:
			// Page down in result
			pageSize := max(1, availableHeight-2)
			maxScroll := max(0, ts.resultLineCount-availableHeight)
			ts.resultScroll += pageSize
			if ts.resultScroll > maxScroll {
				ts.resultScroll = maxScroll
			}
			return ts, nil

		case keyHome:
			// Jump to top of result
			ts.resultScroll = 0
			return ts, nil

		case keyEnd:
			// Jump to bottom of result
			ts.resultScroll = max(0, ts.resultLineCount-availableHeight)
			return ts, nil
		}
	}

	// Special handling for result viewing mode
	if ts.viewingResult && ts.result != nil {
		switch msg.String() {
		case keyUp, "k":
			if ts.resultCursor > 0 {
				ts.resultCursor--
			}
			return ts, nil

		case keyDown, "j":
			if ts.resultCursor < len(ts.resultFields)-1 {
				ts.resultCursor++
			}
			return ts, nil

		case keyEnter, "c", "y":
			// Copy selected field value
			if ts.resultCursor < len(ts.resultFields) {
				field := ts.resultFields[ts.resultCursor]
				if err := ts.copyToClipboard(field.value); err == nil {
					ts.SetStatus(fmt.Sprintf("Copied '%s' to clipboard!", field.path), StatusSuccess)
				} else {
					ts.SetStatus("Failed to copy to clipboard", StatusError)
				}
			}
			return ts, nil

		case "v":
			// Exit result viewing mode
			ts.viewingResult = false
			ts.SetStatus("", StatusInfo)
			return ts, nil

		case keyCtrlC:
			// Copy entire result
			if err := ts.copyToClipboard(ts.resultJSON); err == nil {
				ts.SetStatus("Copied entire result to clipboard!", StatusSuccess)
			} else {
				ts.SetStatus("Failed to copy to clipboard", StatusError)
			}
			return ts, nil

		case keyEsc, "q":
			// Exit result viewing mode
			ts.viewingResult = false
			ts.SetStatus("", StatusInfo)
			return ts, nil
		}

		// Don't process other keys in viewing mode
		return ts, nil
	}

	switch msg.String() {
	case "ctrl+t":
		ts.toggleTaskMode()
		return ts, nil

	case keyToggleArgValidation:
		ts.toggleArgValidation()
		return ts, nil

	case "c":
		// Toggle CLI command display
		if ts.showCLICommand {
			ts.showCLICommand = false
			ts.SetStatus("CLI command hidden", StatusInfo)
		} else {
			ts.cliCommand = ts.generateCLICommand()
			ts.showCLICommand = true
			if err := ts.copyToClipboard(ts.cliCommand); err == nil {
				ts.SetStatus("CLI command copied to clipboard and displayed below!", StatusSuccess)
			} else {
				ts.SetStatus("CLI command displayed below (clipboard copy failed)", StatusWarning)
			}
		}
		return ts, nil

	case keyCtrlC:
		// Copy result to clipboard if available
		switch {
		case ts.result != nil && ts.resultJSON != "":
			if err := ts.copyToClipboard(ts.resultJSON); err == nil {
				ts.SetStatus("Result copied to clipboard!", StatusSuccess)
			} else {
				ts.SetStatus("Failed to copy to clipboard", StatusError)
			}
		case ts.showCLICommand && ts.cliCommand != "":
			// Copy CLI command to clipboard
			if err := ts.copyToClipboard(ts.cliCommand); err == nil {
				ts.SetStatus("CLI command copied to clipboard!", StatusSuccess)
			} else {
				ts.SetStatus("Failed to copy CLI command to clipboard", StatusError)
			}
		default:
			// No result, go back
			return ts, func() tea.Msg { return BackMsg{} }
		}
		return ts, nil

	case "v":
		// Enter result viewing mode if we have results
		if ts.result != nil && len(ts.resultFields) > 0 {
			ts.viewingResult = true
			ts.resultCursor = 0
			ts.SetStatus("Navigate with ↑/↓, Enter to copy field, v/Esc to exit", StatusInfo)
		}
		return ts, nil

	case keyEsc:
		// Go back to previous screen
		return ts, func() tea.Msg { return BackMsg{} }

	case "b", keyAltLeft:
		// Go back to previous screen
		return ts, func() tea.Msg { return BackMsg{} }

	case keyCtrlL, keyCtrlD, keyF12:
		// Show debug logs. Wire the snapshot + notifications providers when
		// a service exists so the Capabilities and Notifications tabs render
		// live data. Tests instantiate ToolScreen with a nil service, so the
		// guard prevents a nil-method-value panic on Ctrl+L.
		debugScreen := NewDebugScreen()
		if ts.mcpService != nil {
			debugScreen.WithSnapshotProvider(ts.mcpService.GetCapabilitiesSnapshot)
			debugScreen.WithNotificationsProvider(ts.mcpService.NotificationStream)
		}
		return ts, func() tea.Msg {
			return ToggleOverlayMsg{
				Screen: debugScreen,
			}
		}

	case keyTab, keyDown:
		// Calculate total items based on mode
		var totalItems int
		var inputCount int
		if ts.rawJSONMode {
			inputCount = 1 // Just the raw JSON input
			totalItems = 4 // raw JSON input + 3 buttons
		} else {
			inputCount = len(ts.fields)
			totalItems = len(ts.fields) + 3 // fields + execute button + cli button + back button
		}

		// Validate and blur current field before moving
		if !ts.rawJSONMode && ts.cursor < inputCount {
			ts.validateField(ts.cursor)
			ts.fields[ts.cursor].input.Blur()
		} else if ts.rawJSONMode && ts.cursor == 0 {
			ts.rawJSONInput.Blur()
		}

		// Move to next field/button
		ts.cursor = (ts.cursor + 1) % totalItems

		// Focus new field if it's an input
		if ts.rawJSONMode && ts.cursor == 0 {
			ts.rawJSONInput.Focus()
		} else if !ts.rawJSONMode && ts.cursor < inputCount {
			ts.fields[ts.cursor].input.Focus()
		}
		return ts, nil

	case keyShiftTab, keyUp:
		// Calculate total items based on mode
		var totalItems int
		var inputCount int
		if ts.rawJSONMode {
			inputCount = 1
			totalItems = 4
		} else {
			inputCount = len(ts.fields)
			totalItems = len(ts.fields) + 3
		}

		// Blur current field
		if !ts.rawJSONMode && ts.cursor < inputCount {
			ts.fields[ts.cursor].input.Blur()
		} else if ts.rawJSONMode && ts.cursor == 0 {
			ts.rawJSONInput.Blur()
		}

		// Move to previous field/button
		ts.cursor = (ts.cursor - 1 + totalItems) % totalItems

		// Focus new field if it's an input
		if ts.rawJSONMode && ts.cursor == 0 {
			ts.rawJSONInput.Focus()
		} else if !ts.rawJSONMode && ts.cursor < inputCount {
			ts.fields[ts.cursor].input.Focus()
		}
		return ts, nil

	case keyEnter:
		// Calculate button positions based on mode
		var executePos, cliPos, backPos int
		if ts.rawJSONMode {
			executePos = 1 // After raw JSON input
			cliPos = 2
			backPos = 3
		} else {
			executePos = len(ts.fields)
			cliPos = len(ts.fields) + 1
			backPos = len(ts.fields) + 2
		}

		// Handle enter based on current position
		switch ts.cursor {
		case executePos:
			// Execute button — gate destructive tools behind a confirm overlay.
			// The check uses the same IsDestructive() helper as the CLI prompt
			// so behavior stays in lock-step across the two surfaces.
			if ts.tool.IsDestructive() && !ts.confirmBypassed {
				ts.pendingConfirm = true
				return ts, openConfirmOverlay(&ts.tool)
			}
			cmd := ts.executeTool()
			return ts, cmd
		case cliPos:
			// CLI button
			ts.cliCommand = ts.generateCLICommand()
			ts.showCLICommand = true

			// Copy to clipboard
			if err := ts.copyToClipboard(ts.cliCommand); err == nil {
				ts.SetStatus("CLI command copied to clipboard and displayed below!", StatusSuccess)
			} else {
				ts.SetStatus("CLI command displayed below (clipboard copy failed)", StatusWarning)
			}
			return ts, nil
		case backPos:
			// Back button
			return ts, func() tea.Msg { return BackMsg{} }
		}
		return ts, nil

	default:
		// Log unhandled keys for debugging
		ts.logger.Info("Unhandled key", debug.F("key", msg.String()), debug.F("cursor", ts.cursor))
		return ts, nil
	}
}

// renderToolBadges produces a colored representation of the tool's
// annotation badges for terminal display. Color coding (per task spec):
//
//	[D] red   — destructive
//	[R] green — readOnly
//	[I] blue  — idempotent
//	[O] gray  — openWorld (informational)
//
// The plain (uncolored) badge string lives on Tool.BadgeString so non-TUI
// callers (CLI list, JSON output, log lines) get a stable string while the
// TUI applies styling.
func renderToolBadges(tool *mcp.Tool) string {
	var out strings.Builder
	dStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))   // red
	rStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))  // green
	iStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))  // blue
	oStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("243")) // gray

	switch {
	case tool.IsDestructive():
		out.WriteString(dStyle.Render("[D]"))
	case tool.IsReadOnly():
		out.WriteString(rStyle.Render("[R]"))
	}
	if tool.IsIdempotent() {
		out.WriteString(iStyle.Render("[I]"))
	}
	if tool.IsOpenWorld() {
		out.WriteString(oStyle.Render("[O]"))
	}
	return out.String()
}

// openConfirmOverlay returns a tea.Cmd that opens the destructive-tool
// confirm overlay. Extracted as a free function so tests can route the
// returned message through the screen without depending on the screen
// manager's wiring.
func openConfirmOverlay(tool *mcp.Tool) tea.Cmd {
	return func() tea.Msg {
		return ToggleOverlayMsg{Screen: NewConfirmScreen(tool)}
	}
}

// executeTool executes the tool with current parameters
func (ts *ToolScreen) executeTool() tea.Cmd {
	args, err := ts.buildArguments()
	if err != nil {
		ts.SetError(err)
		return nil
	}

	ts.executing = true
	ts.executionStart = time.Now()
	ts.showCLICommand = false // Hide CLI command during execution
	if ts.taskMode {
		ts.SetStatus("Calling tool as a task...", StatusInfo)
		return tea.Batch(
			tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return toolSpinnerTickMsg{} }),
			ts.startTaskCmd(args),
		)
	}
	ts.SetStatus("Executing tool...", StatusInfo)
	observe := ts.callProgress.start()

	// Start the execution and spinner ticker
	return tea.Batch(
		// Spinner ticker
		tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
			return toolSpinnerTickMsg{}
		}),
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(mcp.WithProgressObserver(context.Background(), observe), 30*time.Second)
			defer cancel()

			result, err := ts.mcpService.CallTool(ctx, mcp.CallToolRequest{
				Name:      ts.tool.Name,
				Arguments: args,
			})

			return toolExecutionCompleteMsg{
				Result: result,
				Error:  err,
			}
		},
	)
}

// buildArguments turns the form (or the raw JSON editor) into tool call
// arguments, converting each field to the type its schema declares, and
// validates them against the whole input schema: a violation refuses the
// call, or with Ctrl+O is kept in argumentViolation and the call goes out.
func (ts *ToolScreen) buildArguments() (map[string]interface{}, error) {
	ts.argumentViolation = ""
	args, err := ts.formArguments()
	if err != nil {
		return nil, err
	}
	if err := ts.inputSchema.Validate(args); err != nil {
		if !ts.skipArgValidation {
			return nil, fmt.Errorf("%w (Ctrl+O sends them anyway)", err)
		}
		ts.logger.Warn("Sending tool arguments that do not match the input schema",
			debug.F("tool", ts.tool.Name), debug.F("violation", err.Error()))
		ts.argumentViolation = err.Error()
	}
	return args, nil
}

// formArguments reads the arguments out of the form or the raw JSON editor.
func (ts *ToolScreen) formArguments() (map[string]interface{}, error) {
	if ts.rawJSONMode {
		return ts.rawJSONArguments()
	}

	if err := ts.checkRequiredFields(); err != nil {
		return nil, err
	}
	args, _, err := objectFromFields(ts.fields, 0, 0)
	return args, err
}

// objectFromFields builds the object whose fields start at fields[i], depth
// levels down, and returns it with the index of the first field past it.
// An open sub-form builds its object (or list of elements) from its fields;
// a nested object with nothing filled in is left out, while a required
// top-level one is sent empty. Every element in a list is sent.
func objectFromFields(fields []toolField, i, depth int) (obj map[string]interface{}, next int, err error) {
	obj = make(map[string]interface{})
	for i < len(fields) && fields[i].depth == depth {
		var value interface{}
		var send bool
		if value, send, next, err = valueFromFields(fields, i, depth); err != nil {
			return nil, 0, err
		}
		if send {
			obj[fields[i].name] = value
		}
		i = next
	}
	return obj, i, nil
}

// valueFromFields builds the value of fields[i], depth levels down, from
// the field and its open sub-form, and returns the index past them; send
// is false when the value is left out of its object.
func valueFromFields(fields []toolField, i, depth int) (value interface{}, send bool, next int, err error) {
	field := &fields[i]
	sentEmpty := field.required && depth == 0
	switch {
	case field.isElementList():
		elements := []interface{}{}
		next = i + 1
		for next < len(fields) && fields[next].depth == depth+1 {
			var element map[string]interface{}
			if element, next, err = objectFromFields(fields, next+1, depth+2); err != nil {
				return nil, false, 0, err
			}
			elements = append(elements, element)
		}
		return elements, len(elements) > 0 || sentEmpty, next, nil
	case field.expanded:
		var sub map[string]interface{}
		if sub, next, err = objectFromFields(fields, i+1, depth+1); err != nil {
			return nil, false, 0, err
		}
		return sub, len(sub) > 0 || sentEmpty, next, nil
	case field.sendNull:
		return nil, true, i + 1, nil
	}
	text := field.input.Value()
	if text == "" {
		// An empty array is sent only when the field is required.
		return []interface{}{}, sentEmpty && field.fieldType == inputschema.KindArray, i + 1, nil
	}
	converted, err := field.convert(text)
	return converted, err == nil, i + 1, err
}

// rawJSONArguments parses the raw JSON editor; empty means no arguments.
func (ts *ToolScreen) rawJSONArguments() (map[string]interface{}, error) {
	args := make(map[string]interface{})
	rawValue := strings.TrimSpace(ts.rawJSONInput.Value())
	if rawValue == "" {
		return args, nil
	}
	if err := json.Unmarshal([]byte(rawValue), &args); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	return args, nil
}

// rawJSONArgumentWords spells the raw JSON editor's arguments as the CLI's
// key:=<json> words, in key order. Each value keeps the text it was typed
// with (compacted), so a large integer is not rounded through float64. An
// error names JSON the CLI cannot take: not an object, or a key
// inputschema.CheckArgumentKey rejects.
func (ts *ToolScreen) rawJSONArgumentWords() ([]string, error) {
	rawValue := strings.TrimSpace(ts.rawJSONInput.Value())
	if rawValue == "" {
		return nil, nil
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawValue), &args); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		if err := inputschema.CheckArgumentKey(key); err != nil {
			return nil, fmt.Errorf("argument %q: tool call cannot take it: %w", key, err)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	words := make([]string, 0, len(keys))
	for _, key := range keys {
		var value bytes.Buffer
		if err := json.Compact(&value, args[key]); err != nil {
			return nil, fmt.Errorf("argument %q: %v", key, err)
		}
		words = append(words, key+":="+value.String())
	}
	return words, nil
}

// checkRequiredFields reports the first empty required top-level field. A
// sub-form's required fields matter only when its object is sent, which
// the schema check judges. Array fields may be empty (they are sent as []).
func (ts *ToolScreen) checkRequiredFields() error {
	for i := range ts.fields {
		field := &ts.fields[i]
		if field.depth > 0 || field.expanded {
			continue
		}
		if field.required && !field.sendNull && field.input.Value() == "" && field.fieldType != inputschema.KindArray {
			return fmt.Errorf("required field '%s' is empty", field.name)
		}
	}
	return nil
}

// jsonNullLiteral is what a user types to send null.
const jsonNullLiteral = "null"

// isNullLiteral reports that value sends null: the field is nullable and
// not a string (for a string, "null" is text).
func (f *toolField) isNullLiteral(value string) bool {
	return f.nullable && f.fieldType != inputschema.KindString && strings.TrimSpace(value) == jsonNullLiteral
}

// unionKind is the type of a KindUnion field that value's syntax picks.
func (f *toolField) unionKind(value string) (inputschema.Kind, error) {
	return (&inputschema.Param{Name: f.name, Union: f.union}).UnionKind(value)
}

// convert parses a non-empty field value as the field's type.
func (f *toolField) convert(value string) (interface{}, error) {
	if f.isNullLiteral(value) {
		return nil, nil
	}
	switch f.fieldType {
	case inputschema.KindUnion:
		kind, err := f.unionKind(value)
		if err != nil {
			return nil, err
		}
		picked := *f
		picked.fieldType = kind
		return picked.convert(value)
	case inputschema.KindNumber:
		var num float64
		if err := json.Unmarshal([]byte(value), &num); err != nil {
			return nil, fmt.Errorf("invalid number for field '%s'", f.name)
		}
		return num, nil
	case inputschema.KindInteger:
		var num int
		if err := json.Unmarshal([]byte(value), &num); err != nil {
			return nil, fmt.Errorf("invalid integer for field '%s'", f.name)
		}
		return num, nil
	case inputschema.KindBoolean:
		var b bool
		if err := json.Unmarshal([]byte(value), &b); err != nil {
			return nil, fmt.Errorf("invalid boolean for field '%s' (use true/false)", f.name)
		}
		return b, nil
	case inputschema.KindArray:
		return f.convertArray(value)
	case inputschema.KindObject:
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(value), &obj); err != nil {
			return nil, fmt.Errorf("invalid JSON object for field '%s'", f.name)
		}
		return obj, nil
	case inputschema.KindJSON:
		var parsed interface{}
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return value, nil
		}
		return parsed, nil
	default:
		return value, nil
	}
}

// convertArray parses a JSON array, or "a, b, c" when the items are
// strings.
func (f *toolField) convertArray(value string) (interface{}, error) {
	var arr []interface{}
	if err := json.Unmarshal([]byte(value), &arr); err == nil {
		return arr, nil
	}
	if !commaSeparatedItems(f.itemKind) {
		return nil, fmt.Errorf("field '%s' takes a JSON array of %s", f.name, f.itemKind)
	}
	parts := strings.Split(value, ",")
	arr = make([]interface{}, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			arr = append(arr, trimmed)
		}
	}
	return arr, nil
}

// validateField validates a single field
func (ts *ToolScreen) validateField(index int) {
	if index >= len(ts.fields) {
		return
	}

	field := &ts.fields[index]
	field.validationError = ""
	field.inferredKind = ""
	value := field.input.Value()
	if field.sendNull {
		return
	}

	// Check required fields (a sub-form's matter only if its object is sent)
	if field.required && field.depth == 0 && strings.TrimSpace(value) == "" {
		field.validationError = "This field is required"
		return
	}
	if field.isNullLiteral(value) {
		return
	}

	// Type-specific validation
	switch field.fieldType {
	case inputschema.KindUnion:
		if value != "" {
			kind, err := field.unionKind(value)
			if err != nil {
				field.validationError = err.Error()
			}
			field.inferredKind = kind
		}
	case "number":
		if value != "" {
			var num float64
			if err := json.Unmarshal([]byte(value), &num); err != nil {
				field.validationError = "Must be a valid number"
			}
		}
	case "integer":
		if value != "" {
			var num int
			if err := json.Unmarshal([]byte(value), &num); err != nil {
				field.validationError = "Must be a valid integer"
			}
		}
	case "boolean":
		if value != "" {
			if value != boolTrueLiteral && value != "false" {
				field.validationError = "Must be 'true' or 'false'"
			}
		}
	case "array":
		if value != "" {
			var arr []interface{}
			if err := json.Unmarshal([]byte(value), &arr); err != nil {
				switch {
				case !commaSeparatedItems(field.itemKind):
					field.validationError = fmt.Sprintf("Must be a JSON array of %s", field.itemKind)
				case !strings.Contains(value, ","):
					// Try comma-separated format
					field.validationError = "Must be a JSON array or comma-separated values"
				}
			}
		}
	case "object":
		if value != "" {
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(value), &obj); err != nil {
				field.validationError = "Must be a valid JSON object"
			}
		}
	}
}

// View renders the tool screen
func (ts *ToolScreen) View() string {
	header := ts.renderHeader()
	footer := ts.renderFooter()
	result := ts.renderResultBlock(header, footer)
	return header + result + footer
}

// renderHeader builds everything above the result block.
func (ts *ToolScreen) renderHeader() string {
	var builder strings.Builder

	// Title with execution count. Use DisplayName so a server-supplied human
	// title (e.g. "Run Migration") shows in place of a snake_case Name.
	displayName := ts.tool.DisplayName()
	title := fmt.Sprintf("Execute Tool: %s", displayName)
	if ts.executionCount > 0 {
		title = fmt.Sprintf("Execute Tool: %s (Run #%d)", displayName, ts.executionCount+1)
	}
	builder.WriteString(ts.titleStyle.Render(title))
	if badges := ts.tool.BadgeString(); badges != "" {
		builder.WriteString("  ")
		builder.WriteString(renderToolBadges(&ts.tool))
	}
	if ts.taskMode {
		builder.WriteString("  ")
		builder.WriteString(ts.selectedStyle.Render("[task mode]"))
	}
	if ts.skipArgValidation {
		builder.WriteString("  ")
		builder.WriteString(ts.warningStyle.Render(argValidationOffBadge))
	}
	builder.WriteString("\n")
	if ts.argumentViolation != "" {
		builder.WriteString(ts.warningStyle.Render("⚠ Sent despite the input schema: " + ts.argumentViolation))
		builder.WriteString("\n")
	}
	if ts.runningTask != nil {
		builder.WriteString(ts.labelStyle.Render(taskProgressLine(ts.runningTask)))
		builder.WriteString("\n")
	}

	if ts.tool.Description != "" {
		builder.WriteString(ts.labelStyle.Render(ts.tool.Description))
		builder.WriteString("\n")
	}
	builder.WriteString("\n")

	// Show schema error warning banner if applicable
	if ts.tool.HasSchemaError() {
		warningStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("11")).
			Padding(0, 1)
		errorMsgStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Italic(true)

		builder.WriteString(warningStyle.Render("⚠ Schema Error - Raw JSON Mode"))
		builder.WriteString("\n")
		builder.WriteString(errorMsgStyle.Render(ts.tool.SchemaError.Message))
		builder.WriteString("\n\n")
	}
	if ts.schemaNote != "" {
		builder.WriteString(ts.labelStyle.Render("ℹ Input schema not shown as a form - Raw JSON Mode"))
		builder.WriteString("\n")
		builder.WriteString(ts.labelStyle.Render(ts.schemaNote))
		builder.WriteString("\n\n")
	}

	// Raw JSON mode - show single input for JSON arguments
	switch {
	case ts.rawJSONMode:
		builder.WriteString(ts.labelStyle.Render("Arguments (JSON):"))
		builder.WriteString("\n")
		inputView := ts.rawJSONInput.View()
		if ts.cursor == 0 {
			builder.WriteString(ts.selectedStyle.Render(inputView))
		} else {
			builder.WriteString(ts.inputStyle.Render(inputView))
		}
		builder.WriteString("\n\n")
	case len(ts.fields) == 0:
		// Form fields or message if no fields
		builder.WriteString(ts.labelStyle.Render("This tool requires no parameters."))
		builder.WriteString("\n\n")
	default:
		for i := range ts.fields {
			field := &ts.fields[i]
			// Field label with type indicator, indented by sub-form depth
			label := strings.Repeat("  ", field.depth) + field.name
			if field.required {
				label += " *"
			}

			// Always show field type for clarity
			typeIndicator := string(field.fieldType)
			if field.fieldType == inputschema.KindUnion {
				typeIndicator = (&inputschema.Param{Union: field.union}).UnionLabel()
				if field.inferredKind != "" {
					typeIndicator += " → " + string(field.inferredKind)
				}
			}
			if field.itemKind != "" {
				typeIndicator += " of " + string(field.itemKind)
			}
			if field.nullable {
				typeIndicator += "|null"
			}
			label += fmt.Sprintf(" [%s]", typeIndicator)

			if field.description != "" {
				label += fmt.Sprintf(" - %s", field.description)
			}
			if field.note != "" {
				label += fmt.Sprintf(" (%s)", field.note)
			}
			if field.hasSubForm() && !field.expanded {
				label += " (Ctrl+E: sub-form)"
			}
			builder.WriteString(ts.labelStyle.Render(label + ":"))
			builder.WriteString("\n")

			// Render the textinput model
			inputView := field.input.View()
			switch {
			case field.sendNull:
				inputView = "null (Ctrl+N to edit)"
			case field.element:
				inputView = "▾ element (Ctrl+X: remove)"
			case field.isElementList():
				inputView = "▾ elements below (Ctrl+A: add one, Ctrl+E: type as JSON)"
			case field.expanded:
				inputView = "▾ filled in below (Ctrl+E: type as JSON)"
			}

			// Apply styling based on focus and validation
			switch {
			case field.validationError != "" && ts.cursor == i:
				// Red border for validation errors
				errorStyle := lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("9")).
					Padding(0, 1).
					Width(60)
				builder.WriteString(errorStyle.Render(inputView))
			case ts.cursor == i:
				// Focused style
				builder.WriteString(ts.selectedStyle.Render(inputView))
			default:
				// Normal style
				builder.WriteString(ts.inputStyle.Render(inputView))
			}
			builder.WriteString("\n")

			// Show validation error message
			if field.validationError != "" {
				validationStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("9")).
					Italic(true)
				builder.WriteString(validationStyle.Render("  ⚠ " + field.validationError))
				builder.WriteString("\n")
			}
			builder.WriteString("\n")
		}
	}

	// Buttons - calculate positions based on mode
	var executePos, cliPos, backPos int
	if ts.rawJSONMode {
		executePos = 1
		cliPos = 2
		backPos = 3
	} else {
		executePos = len(ts.fields)
		cliPos = len(ts.fields) + 1
		backPos = len(ts.fields) + 2
	}

	executeBtn := " Execute "
	cliBtn := " CLI "
	backBtn := " Back "

	if ts.cursor == executePos {
		builder.WriteString(ts.selectedButtonStyle.Render(executeBtn))
	} else {
		builder.WriteString(ts.buttonStyle.Render(executeBtn))
	}
	builder.WriteString("  ")
	if ts.cursor == cliPos {
		builder.WriteString(ts.selectedButtonStyle.Render(cliBtn))
	} else {
		builder.WriteString(ts.buttonStyle.Render(cliBtn))
	}
	builder.WriteString("  ")
	if ts.cursor == backPos {
		builder.WriteString(ts.selectedButtonStyle.Render(backBtn))
	} else {
		builder.WriteString(ts.buttonStyle.Render(backBtn))
	}
	builder.WriteString("\n\n")

	// Execution status with progress indicator
	if ts.executing {
		elapsed := time.Since(ts.executionStart)

		// Show spinner and message
		builder.WriteString(components.ProgressMessage("Executing tool...", elapsed, true))
		builder.WriteString("\n")

		// The server's progress when it reports any, else an
		// indeterminate bar.
		if line := ts.callProgress.line(); line != "" {
			builder.WriteString(line)
		} else {
			builder.WriteString(components.NewIndeterminateProgress(40).Render(elapsed))
		}
		builder.WriteString("\n")

		// Show timeout warning if taking too long
		if elapsed > 10*time.Second {
			warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
			remaining := 30*time.Second - elapsed
			if remaining > 0 {
				builder.WriteString(warningStyle.Render(fmt.Sprintf("Timeout in %s", remaining.Round(time.Second))))
			} else {
				builder.WriteString(warningStyle.Render("Operation may timeout soon..."))
			}
			builder.WriteString("\n")
		}
	} else if line := ts.callProgress.line(); ts.runningTask != nil && line != "" {
		// A 2025-11-25 task reporting progress on its call's token.
		builder.WriteString(line + "\n")
	}

	return builder.String()
}

// renderResultBlock builds the result section, sized to fill remaining
// vertical space between the header and footer.
func (ts *ToolScreen) renderResultBlock(header, footer string) string {
	if ts.result == nil {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("\n")

	// Show execution header with count and timestamp
	execInfoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("99")).
		Bold(true)

	execInfo := fmt.Sprintf("Execution #%d", ts.executionCount)
	if ts.executionCount > 1 {
		execInfo = fmt.Sprintf("✨ Execution #%d", ts.executionCount)
	}
	execInfo += fmt.Sprintf(" • %s", ts.lastExecution.Format("15:04:05"))

	builder.WriteString(execInfoStyle.Render(execInfo))
	builder.WriteString("\n")

	// isError:true is the v1.5.0 channel for tool-layer errors (e.g. input
	// validation failures, business-rule violations) — the call completed
	// and the server responded with a structured payload tagged as an error.
	// We render a red, padded banner above the result body to make this
	// distinct from:
	//   - JSON-RPC protocol errors (handled separately via ts.LastError(),
	//     rendered in the footer with a different "Error: <message>" format)
	//   - outputSchema violations (rendered just below as a yellow banner)
	// The banner header still reads "Error Result:" inline so the result
	// label stays unambiguous when the user reads top-down.
	if ts.result.IsError {
		errBannerStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")). // white text
			Background(lipgloss.Color("9")).  // red background
			Padding(0, 1)
		builder.WriteString(errBannerStyle.Render("⚠ Tool reported an error (isError:true)"))
		builder.WriteString("\n")
		builder.WriteString(ts.errorStyle.Render("Error Result:"))
	} else {
		builder.WriteString(ts.labelStyle.Render("Result:"))
	}
	builder.WriteString("\n")

	// outputSchema violations (Tier 2 schema validation) are surfaced as a
	// yellow warning banner above the result body so the operator notices
	// the mismatch before reading the (possibly malformed) payload. The
	// banner is intentionally non-blocking — the result still renders below
	// — because the spec calls these "warnings, not errors": consumers may
	// still want to see the data, they just need to know the contract was
	// not honored.
	if violations := ts.result.OutputViolations; len(violations) > 0 {
		// Yellow + bold matches the schema-error warning palette used
		// elsewhere on this screen so the visual treatment is consistent.
		warnStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("220"))
		bulletStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("220"))
		violationsHeader := fmt.Sprintf("⚠ Output schema violations (%d):", len(violations))
		builder.WriteString(warnStyle.Render(violationsHeader))
		builder.WriteString("\n")
		for _, v := range violations {
			builder.WriteString(bulletStyle.Render("  • " + v))
			builder.WriteString("\n")
		}
	}

	headerH := lipgloss.Height(header)
	footerH := lipgloss.Height(footer)
	availableHeight := ts.computeResultDisplayHeight(headerH, footerH)

	// The round trace sits under the result body; shrink the body so the
	// trace stays on screen.
	roundTrace := renderResultTrailer(ts.result.Rounds, ts.result.Server)
	if roundTrace != "" {
		availableHeight = max(resultMinHeight, availableHeight-lipgloss.Height(roundTrace)-1)
	}

	termWidth := ts.Width()
	if termWidth == 0 {
		termWidth = defaultTermWidth
	}

	if ts.viewingResult && len(ts.resultFields) > 0 {
		fieldStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		selectedFieldStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("240")).
			Foreground(lipgloss.Color("15")).
			Bold(true)
		pathStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Bold(true)
		valueStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("10"))

		builder.WriteString(fieldStyle.Render("Select a field to copy its value:"))
		builder.WriteString("\n\n")

		for i, field := range ts.resultFields {
			var line string
			if i == ts.resultCursor {
				line = fmt.Sprintf("▶ %s = %s",
					pathStyle.Render(field.path),
					valueStyle.Render(field.value))
				builder.WriteString(selectedFieldStyle.Render(line))
			} else {
				line = fmt.Sprintf("  %s = %s",
					pathStyle.Render(field.path),
					valueStyle.Render(field.value))
				builder.WriteString(line)
			}
			builder.WriteString("\n")
		}

		viewHelpStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)
		builder.WriteString("\n")
		builder.WriteString(viewHelpStyle.Render(
			"↑/↓: Navigate • Enter/c/y: Copy field • Ctrl+C: Copy all • v/Esc: Exit view"))
	} else {
		lines := ts.resultLines
		if lines == nil {
			lines = []string{}
		}

		startIdx := ts.resultScroll
		endIdx := startIdx + availableHeight
		if startIdx >= len(lines) {
			startIdx = max(0, len(lines)-1)
		}
		if endIdx > len(lines) {
			endIdx = len(lines)
		}
		visibleLines := lines[startIdx:endIdx]

		resultStyle := ts.resultStyle.
			Width(termWidth - resultWidthMargin).
			Height(availableHeight)

		resultContent := strings.Join(visibleLines, "\n")
		builder.WriteString(resultStyle.Render(resultContent))

		if len(lines) > availableHeight {
			builder.WriteString("\n")

			scrollStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Italic(true)

			canScrollUp := startIdx > 0
			canScrollDown := endIdx < len(lines)

			var indicator string
			switch {
			case canScrollUp && canScrollDown:
				indicator = fmt.Sprintf("↑ Ctrl+Up/Down: Scroll (line %d-%d/%d) ↓", startIdx+1, endIdx, len(lines))
			case canScrollUp:
				indicator = fmt.Sprintf("↑ Ctrl+Up: Scroll up (line %d-%d/%d)", startIdx+1, endIdx, len(lines))
			case canScrollDown:
				indicator = fmt.Sprintf("Ctrl+Down: Scroll down (line %d-%d/%d) ↓", startIdx+1, endIdx, len(lines))
			default:
				indicator = fmt.Sprintf("Line %d-%d/%d", startIdx+1, endIdx, len(lines))
			}

			builder.WriteString(scrollStyle.Render(indicator))
		}

		if len(ts.resultFields) > 1 {
			builder.WriteString("\n")
			hintStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Italic(true)
			builder.WriteString(hintStyle.Render("Press 'v' to view fields • Ctrl+↑/↓, PgUp/PgDn, Home/End: Scroll"))
		}
	}
	builder.WriteString("\n")
	if roundTrace != "" {
		builder.WriteString(roundTrace)
		builder.WriteString("\n")
	}

	return builder.String()
}

// renderFooter builds everything below the result block.
func (ts *ToolScreen) renderFooter() string {
	var builder strings.Builder

	// CLI command display
	if ts.showCLICommand && ts.cliCommand != "" {
		builder.WriteString("\n")

		// CLI command header
		cliHeaderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")). // Cyan
			Bold(true)
		builder.WriteString(cliHeaderStyle.Render("Equivalent CLI Command (POSIX shell):"))
		builder.WriteString("\n")

		// CLI command box - no fixed width to prevent wrapping
		// Let the content determine the natural width
		cliCommandStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("6")). // Cyan border
			Padding(1).
			Width(0).                        // No wrapping - let content define width naturally
			Foreground(lipgloss.Color("15")) // White text

		builder.WriteString(cliCommandStyle.Render(ts.cliCommand))
		builder.WriteString("\n")

		// CLI command help
		cliHelpStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)
		builder.WriteString(cliHelpStyle.Render("Copy this command to run the same tool call from the command line"))
		builder.WriteString("\n")
	}

	// Error message
	if err := ts.LastError(); err != nil {
		builder.WriteString("\n")
		builder.WriteString(ts.errorStyle.Render(fmt.Sprintf("Error: %v", err)))
		builder.WriteString("\n")
	}

	// Help text
	builder.WriteString("\n")
	var helpText string
	switch {
	case ts.viewingResult:
		// Already shown inline help for viewing mode
		helpText = ""
	case ts.result != nil:
		if len(ts.resultFields) > 1 {
			helpText = "v: View fields • c: CLI command • Ctrl+C: Copy all • Ctrl+↑/↓: Scroll • " +
				"Ctrl+L: Debug Log • b/Alt+←: Back • Esc: Back"
		} else {
			helpText = "c: CLI command • Ctrl+C: Copy result • Ctrl+↑/↓, PgUp/PgDn, Home/End: Scroll • " +
				"Ctrl+L: Debug Log • b/Alt+←: Back • Esc: Back"
		}
	case ts.cursor < len(ts.fields):
		helpText = "Tab: Navigate • Enter: Submit • c: CLI command • Ctrl+V: Paste • Ctrl+T: Task mode • " +
			"Ctrl+O: Send schema violations • Ctrl+L: Debug Log • b: Back • Esc: Back"
		if f := ts.fields[ts.cursor]; f.nullable && !f.expanded {
			helpText = "Ctrl+N: Null • " + helpText
		}
		if ts.fields[ts.cursor].hasSubForm() {
			helpText = "Ctrl+E: Sub-form • " + helpText
		}
		if list, element := ts.enclosingElementList(ts.cursor); element >= 0 {
			helpText = "Ctrl+A: Add element • Ctrl+X: Remove element • " + helpText
		} else if list >= 0 {
			helpText = "Ctrl+A: Add element • " + helpText
		}
	case ts.cursor == len(ts.fields):
		helpText = "Enter: Execute • Tab: Navigate • c: CLI command • Ctrl+T: Task mode • " +
			"Ctrl+O: Send schema violations • Ctrl+L: Debug Log • b: Back • Esc: Back"
	case ts.cursor == len(ts.fields)+1:
		helpText = "Enter: Show CLI command • Tab: Navigate • c: CLI toggle • Ctrl+L: Debug Log • b: Back • Esc: Back"
	default:
		helpText = "Tab: Navigate • Enter: Go back • c: CLI command • Ctrl+L: Debug Log • b/Alt+←: Back • Esc: Back"
	}
	if helpText != "" {
		builder.WriteString(ts.helpStyle.Render(helpText))
	}

	// Status message
	if statusMsg, level := ts.StatusMessage(); statusMsg != "" {
		builder.WriteString("\n\n")
		var statusColor string
		switch level {
		case StatusSuccess:
			statusColor = "10" // green
		case StatusWarning:
			statusColor = "11" // yellow
		case StatusError:
			statusColor = "9" // red
		default:
			statusColor = "12" // blue
		}
		statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Bold(true)
		builder.WriteString(statusStyle.Render(statusMsg))
	}

	return builder.String()
}

// parseResultFields extracts copyable fields from JSON result
func (ts *ToolScreen) parseResultFields() {
	ts.resultFields = []resultField{}

	// Try to parse as JSON
	var data interface{}
	if err := json.Unmarshal([]byte(ts.resultJSON), &data); err != nil {
		// Not JSON, treat as single text field
		ts.resultFields = append(ts.resultFields, resultField{
			path:  "result",
			value: ts.resultJSON,
			raw:   ts.resultJSON,
		})
		return
	}

	// Recursively extract fields
	ts.extractFields("", data)

	// Sort fields by path for consistent ordering
	sort.Slice(ts.resultFields, func(i, j int) bool {
		return ts.resultFields[i].path < ts.resultFields[j].path
	})
}

// extractFields recursively extracts fields from JSON data
func (ts *ToolScreen) extractFields(prefix string, data interface{}) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}

			switch val := value.(type) {
			case map[string]interface{}, []interface{}:
				// Recurse into nested structures
				ts.extractFields(path, val)
			default:
				// Leaf value
				strVal := fmt.Sprintf("%v", value)
				if strVal != "" && strVal != "null" {
					ts.resultFields = append(ts.resultFields, resultField{
						path:  path,
						value: strVal,
						raw:   value,
					})
				}
			}
		}

	case []interface{}:
		for i, item := range v {
			path := fmt.Sprintf("%s[%d]", prefix, i)
			ts.extractFields(path, item)
		}

	default:
		// Leaf value
		strVal := fmt.Sprintf("%v", v)
		if strVal != "" && strVal != "null" && prefix != "" {
			ts.resultFields = append(ts.resultFields, resultField{
				path:  prefix,
				value: strVal,
				raw:   v,
			})
		}
	}
}
