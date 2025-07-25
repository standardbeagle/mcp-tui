# Implementation Patterns

## Service Layer Patterns

### 1. MCP Operation Pattern (From Tool Implementation)
**Pattern Location**: `internal/mcp/service.go:371-441` (CallTool implementation)

**Pattern Structure**:
```go
func (s *service) Operation(ctx context.Context, req RequestType) (*ResponseType, error) {
    // 1. Connection validation
    if !s.IsConnected() {
        return nil, fmt.Errorf("not connected to MCP server")
    }
    
    // 2. Request ID generation and logging
    s.mu.Lock()
    requestID := s.getNextRequestID()
    s.mu.Unlock()
    logMCPRequest("method_name", params, requestID)
    
    // 3. SDK operation call
    result, err := s.sessionManager.CurrentSession().Client().Method(ctx, params)
    
    // 4. Error handling and classification
    if err != nil {
        logMCPError(-1, err.Error(), requestID)
        return nil, s.errorHandler.ClassifyAndHandle(err, "operation_context")
    }
    
    // 5. Response logging and return
    logMCPResponse(result, requestID)
    return &ResponseType{result}, nil
}
```

**Apply To**: `GetResource()` implementation, enhanced prompt operations

### 2. Error Handling Pattern
**Pattern Location**: `internal/mcp/errors/handler.go`

**Pattern Structure**:
```go
// Error classification with context
err = s.errorHandler.ClassifyAndHandle(err, "prompt_execution")

// Error wrapping with operation context
return fmt.Errorf("failed to execute prompt '%s': %w", promptName, err)
```

**Apply To**: All new prompt and resource operations

## TUI Screen Patterns

### 3. Screen Creation Pattern (From Tool Screen)
**Pattern Location**: `internal/tui/screens/tool.go:84-100` (NewToolScreen)

**Pattern Structure**:
```go
func NewContentScreen(content ContentType, service mcp.Service) *ContentScreen {
    cs := &ContentScreen{
        BaseScreen: NewBaseScreen("ScreenName", true),
        logger:     debug.Component("screen-name"),
        content:    content,
        mcpService: service,
    }
    
    // Initialize styles
    cs.initStyles()
    
    // Parse content schema/structure
    cs.parseContent()
    
    return cs
}
```

**Apply To**: `NewPromptScreen()`, `NewResourceScreen()`

### 4. Update Message Handling Pattern
**Pattern Location**: `internal/tui/screens/tool.go:346-424` (Update method)

**Pattern Structure**:
```go
func (s *Screen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        s.UpdateSize(msg.Width, msg.Height)
        return s, nil
        
    case tea.KeyMsg:
        return s.handleKeyMsg(msg)
        
    case operationCompleteMsg:
        s.executing = false
        if msg.Error != nil {
            s.SetError(msg.Error)
        } else {
            s.result = msg.Result
            s.SetStatus("Operation completed successfully", StatusSuccess)
        }
        return s, nil
    }
    return s, nil
}
```

**Apply To**: All new screen implementations

### 5. Progress Integration Pattern (From Tool Screen)
**Pattern Location**: `internal/tui/screens/tool.go:928-951` (View method)

**Pattern Structure**:
```go
// Show progress during execution
if s.executing {
    elapsed := time.Since(s.executionStart)
    
    // Show spinner and message
    builder.WriteString(components.ProgressMessage("Operation in progress...", elapsed, true))
    builder.WriteString("\n")
    
    // Show indeterminate progress bar
    progressBar := components.NewIndeterminateProgress(40)
    builder.WriteString(progressBar.Render(elapsed))
    builder.WriteString("\n")
    
    // Show timeout warning if taking too long
    if elapsed > 10*time.Second {
        // Warning display logic
    }
}
```

**Apply To**: All long-running operations (prompt execution, resource loading)

## CLI Command Patterns

### 6. CLI Command Structure (From Tool Commands)
**Pattern Location**: `internal/cli/tool.go:64-80` (CreateCommand)

**Pattern Structure**:
```go
func (tc *ContentCommand) CreateCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "content",
        Short: "Interact with MCP server content",
        Long:  "List, describe, and interact with content from MCP server",
    }
    
    // Add output format flag to all subcommands
    cmd.PersistentFlags().StringP("output", "o", "text", "Output format (text, json)")
    
    // Add subcommands
    cmd.AddCommand(tc.createListCommand())
    cmd.AddCommand(tc.createGetCommand())
    cmd.AddCommand(tc.createExecuteCommand()) // For prompts
    
    return cmd
}
```

**Apply To**: `cmd_prompt.go`, `cmd_resource.go`

### 7. JSON Output Pattern (Recently Added)
**Pattern Location**: `internal/cli/tool.go:157-170` (handleList with JSON)

**Pattern Structure**:
```go
// Handle JSON output format
if tc.GetOutputFormat() == OutputFormatJSON {
    outputData := map[string]interface{}{
        "items": items,
        "count": len(items),
    }
    
    jsonBytes, err := json.MarshalIndent(outputData, "", "  ")
    if err != nil {
        return fmt.Errorf("failed to marshal to JSON: %w", err)
    }
    
    fmt.Println(string(jsonBytes))
    return nil
}
```

**Apply To**: All new CLI command handlers

## Progress Component Patterns

### 8. Spinner Integration Pattern
**Pattern Location**: `internal/tui/components/progress.go:100-110` (ProgressMessage)

**Pattern Structure**:
```go
func ProgressMessage(message string, elapsed time.Duration, showSpinner bool) string {
    timeStr := formatDuration(elapsed)
    
    if showSpinner {
        spinner := NewSpinner(SpinnerLine)
        spinnerFrame := spinner.Frame(elapsed)
        return fmt.Sprintf("%s %s (%s)", spinnerFrame, message, timeStr)
    }
    
    return fmt.Sprintf("%s (%s)", message, timeStr)
}
```

**Usage Pattern**:
```go
// In screen View() method during operations
if s.loading {
    elapsed := time.Since(s.loadStart)
    builder.WriteString(components.ProgressMessage("Loading content...", elapsed, true))
}
```

**Apply To**: Resource loading, prompt execution, any network operations

### 9. Async Operation Pattern (From Tool Screen)
**Pattern Location**: `internal/tui/screens/tool.go:737-767` (executeTool function)

**Pattern Structure**:
```go
func (s *Screen) executeOperation() tea.Cmd {
    s.executing = true
    s.executionStart = time.Now()
    s.SetStatus("Executing operation...", StatusInfo)
    
    return tea.Batch(
        // Spinner ticker for UI updates
        tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
            return spinnerTickMsg{}
        }),
        // Actual operation with minimum display time
        func() tea.Msg {
            startTime := time.Now()
            
            ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
            defer cancel()
            
            result, err := s.performOperation(ctx)
            
            // Ensure operation is visible for at least 500ms
            elapsed := time.Since(startTime)
            if elapsed < 500*time.Millisecond {
                time.Sleep(500*time.Millisecond - elapsed)
            }
            
            return operationCompleteMsg{Result: result, Error: err}
        },
    )
}
```

**Apply To**: All async operations in new screens

## Content Display Patterns

### 10. Tab Navigation Pattern (From Main Screen)
**Pattern Location**: `internal/tui/screens/main.go:33-44` (UI state)

**Pattern Structure**:
```go
type MainScreen struct {
    activeTab   int        // 0=tools, 1=resources, 2=prompts, 3=events
    
    // Content arrays per tab
    tools       []mcp.Tool
    resources   []string
    prompts     []string
    events      []debug.MCPLogEntry
    
    // Loading states per tab
    toolsLoading     bool
    resourcesLoading bool
    promptsLoading   bool
    
    // Selection state per tab
    selectedIndex map[int]int
}
```

**Navigation Handling**:
```go
case "tab", "right":
    ms.activeTab = (ms.activeTab + 1) % 4
case "shift+tab", "left":
    ms.activeTab = (ms.activeTab - 1 + 4) % 4
```

**Apply To**: Enhanced tab system, content-specific navigation

### 11. Content Loading Pattern (From Main Screen)
**Pattern Location**: `internal/tui/screens/main.go:ItemsLoadedMsg` handling

**Pattern Structure**:
```go
// Message types for async loading
type ContentLoadedMsg struct {
    ContentType int
    Items       []ContentItem
    Error       error
}

// Loading initiation
func (s *Screen) loadContent(contentType int) tea.Cmd {
    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        
        items, err := s.mcpService.LoadContent(ctx, contentType)
        return ContentLoadedMsg{
            ContentType: contentType,
            Items:       items,
            Error:       err,
        }
    }
}
```

**Apply To**: Resource content loading, prompt loading

## Error Handling Patterns

### 12. User-Friendly Error Display
**Pattern Location**: `internal/tui/screens/tool.go:1069-1073` (Error display in View)

**Pattern Structure**:
```go
// Error message display
if err := s.LastError(); err != nil {
    builder.WriteString("\n")
    builder.WriteString(s.errorStyle.Render(fmt.Sprintf("Error: %v", err)))
    builder.WriteString("\n")
}
```

**Status Message Pattern**:
```go
// Status with appropriate level
s.SetStatus("Operation completed successfully", StatusSuccess)
s.SetStatus("Warning: Partial results", StatusWarning)  
s.SetStatus("Operation failed", StatusError)
```

**Apply To**: All error handling in new screens

### 13. Operation Timeout Pattern
**Pattern Location**: `internal/tui/screens/tool.go:941-951` (Timeout warning)

**Pattern Structure**:
```go
// Show timeout warning if operation takes too long
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
```

**Apply To**: All operations with timeout potential