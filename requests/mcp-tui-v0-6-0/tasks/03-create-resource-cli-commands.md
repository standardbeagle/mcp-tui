# Task: Create Resource CLI Commands

**Generated from Master Planning**: 2025-01-25
**Context Package**: `/requests/mcp-tui-v0-6-0/context/`
**Next Phase**: [subtasks-execute.md](subtasks-execute.md)

## Task Sizing Assessment
**File Count**: 3 files - New command file + main registration + test file
**Estimated Time**: 25 minutes - Following established tool command patterns
**Token Estimate**: 85k tokens - Complete CLI command implementation
**Complexity Level**: 2 (Moderate) - Replicating tool CLI pattern with resource specifics
**Parallelization Benefit**: HIGH - Can run fully parallel with prompt CLI task
**Atomicity Assessment**: ✅ ATOMIC - Complete resource CLI functionality
**Boundary Analysis**: ✅ CLEAR - CLI layer only, depends on GetResource method

## Persona Assignment
**Persona**: Software Engineer  
**Expertise Required**: Cobra CLI framework, Go interfaces, content display
**Worktree**: `~/work/worktrees/mcp-tui-v0-6-0/03-resource-cli/`

## Context Summary
**Risk Level**: LOW - Following proven tool CLI pattern
**Integration Points**: Uses service layer GetResource method, integrates with main CLI
**Architecture Pattern**: CLI command structure (tool.go:64-80)
**Similar Reference**: `internal/cli/tool.go` - complete command implementation

### Codebase Context (from master analysis)
**Files in Scope**:
```yaml
read_files:   [/internal/cli/tool.go, /internal/cli/base.go, /main.go]
modify_files: [/main.go] 
create_files: [/internal/cli/cmd_resource.go, /internal/cli/resource_test.go]
```

**Existing Patterns to Follow**:
- `/internal/cli/tool.go:64-80` - CLI command structure and organization
- `/internal/cli/tool.go:157-170` - JSON output format handling  
- `/internal/cli/tool.go:130-194` - List operation with progress messages

**Dependencies Context**:
- `github.com/spf13/cobra` - CLI framework
- Task 01 GetResource method - required for resource get command

## Task Scope Boundaries

**MODIFY Zone** (Direct Changes):
```yaml
primary_files:
  - /internal/cli/cmd_resource.go         # New resource command implementation
  - /main.go                              # Register new resource command

create_files:
  - /internal/cli/cmd_resource.go         # Complete resource CLI implementation
  - /internal/cli/resource_test.go        # Unit tests for resource commands
```

**REVIEW Zone** (Check for Impact):
```yaml
check_integration:
  - /internal/cli/base.go                 # Verify BaseCommand compatibility
  - /internal/mcp/service.go              # Ensure GetResource method available
```

**IGNORE Zone** (Do Not Touch):
```yaml
ignore_completely:
  - /internal/tui/screens/*               # TUI layer
  - /internal/cli/tool.go                 # Existing tool commands
  - /examples/*                           # Example configurations
  - /internal/debug/*                     # Debug infrastructure

ignore_search_patterns:
  - "**/node_modules/**"
  - "**/build/**" 
  - "**/dist/**"
  - "**/*.min.js"
```

**Boundary Analysis Results**:
- **Usage Count**: New functionality - no existing usage
- **Scope Assessment**: LIMITED scope - CLI layer only
- **Impact Radius**: 2 files in MODIFY zone, 2 files in REVIEW zone

## External Context Sources (from master research)

**Primary Documentation**:
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-06-18) - Resources section 4.3 - Resource operations and URI handling
- [Cobra CLI Framework](https://cobra.dev/) - Command structure and argument handling

**Standards Applied**:
- [CLI Application Standards](https://www.gnu.org/prep/standards/standards.html#Command_002dLine-Interfaces) - URI argument handling and content display
- Project JSON output format - Consistency with tool/prompt commands

**Reference Implementation**:
- Tool CLI commands (tool.go) - Complete pattern to replicate - Structure, error handling, progress display

## Task Requirements

**Objective**: Create complete resource CLI commands for listing and retrieving resource content

**Success Criteria**:
- [ ] ResourceCommand struct following ToolCommand pattern (tool.go:15-18)
- [ ] Command structure with list and get subcommands (no execute for resources)
- [ ] JSON output support for all subcommands (--output flag)
- [ ] URI argument handling for resource get command
- [ ] Content display handling for various resource types (text, binary)
- [ ] Error handling following BaseCommand.HandleError pattern
- [ ] Progress messages suppressed in JSON mode
- [ ] Main.go registration following createToolCommand pattern
- [ ] Unit tests covering list and get operations

**Validation Commands**:
```bash
# Command Registration Verification
./mcp-tui resource --help                                  # Command exists and shows help
./mcp-tui resource list --help                             # List subcommand exists
./mcp-tui resource get --help                              # Get subcommand exists  
./mcp-tui resource list --output json | jq .              # JSON output works
go test internal/cli/resource_test.go                      # Tests pass
```

## Risk Mitigation (from master analysis)

**High-Risk Mitigations**:
- Resource content display for large/binary content - Implement size limits and appropriate formatting

**Context Validation**:
- [ ] Tool command pattern from tool.go successfully adapted
- [ ] JSON output format matches existing commands
- [ ] Content display handles text and binary appropriately

## Integration with Other Tasks

**Dependencies**: Task 01 (GetResource method) - required for get command functionality
**Integration Points**: Uses service ListResources/GetResource methods
**Shared Context**: CLI pattern shared with prompt CLI task

## Documentation from Master Context

### Implementation Pattern Reference
From `/context/implementation-patterns.md` - CLI Command Structure:

```go
// Resource-specific command structure
type ResourceCommand struct {
    *BaseCommand
}

func NewResourceCommand() *ResourceCommand {
    return &ResourceCommand{
        BaseCommand: NewBaseCommand(),
    }
}

func (rc *ResourceCommand) CreateCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "resource",
        Short: "Interact with MCP server resources",
        Long:  "List and retrieve resources provided by the MCP server",
    }
    
    // Add output format flag
    cmd.PersistentFlags().StringP("output", "o", "text", "Output format (text, json)")
    
    // Add subcommands (no execute for resources)
    cmd.AddCommand(rc.createListCommand())
    cmd.AddCommand(rc.createGetCommand())
    
    return cmd
}
```

### Command Structure Required
```bash
mcp-tui resource list [--output json]                     # List all resources
mcp-tui resource get <uri> [--output json]               # Get resource content
```

### Content Display Strategy
- **Text Resources**: Display content directly with size limits
- **Binary Resources**: Show hex dump preview or indicate binary content
- **Large Resources**: Truncate with size information
- **JSON Mode**: Include content, metadata, and size information

## Execution Notes
- **Start Pattern**: Copy tool.go structure, remove execute command, adapt for resources
- **Key Context**: Resource get uses URI parameter instead of name
- **Content Handling**: Implement appropriate display for different content types
- **Review Focus**: Ensure URI handling and content display work correctly