# Task: Create Prompt CLI Commands

**Generated from Master Planning**: 2025-01-25
**Context Package**: `/requests/mcp-tui-v0-6-0/context/`
**Next Phase**: [subtasks-execute.md](subtasks-execute.md)

## Task Sizing Assessment
**File Count**: 3 files - New command file + main registration + base command enhancement
**Estimated Time**: 25 minutes - Following established tool command patterns
**Token Estimate**: 90k tokens - Complete CLI command implementation
**Complexity Level**: 2 (Moderate) - Replicating established tool CLI patterns
**Parallelization Benefit**: MEDIUM - Can run parallel with resource CLI task
**Atomicity Assessment**: ✅ ATOMIC - Complete prompt CLI functionality
**Boundary Analysis**: ✅ CLEAR - CLI layer only, no TUI dependencies

## Persona Assignment
**Persona**: Software Engineer
**Expertise Required**: Cobra CLI framework, Go interfaces, JSON output
**Worktree**: `~/work/worktrees/mcp-tui-v0-6-0/02-prompt-cli/`

## Context Summary
**Risk Level**: LOW - Replicating proven tool CLI pattern exactly
**Integration Points**: Uses existing service layer, integrates with main CLI
**Architecture Pattern**: CLI command structure (tool.go:64-80)
**Similar Reference**: `internal/cli/tool.go` - complete command implementation

### Codebase Context (from master analysis)
**Files in Scope**:
```yaml
read_files:   [/internal/cli/tool.go, /internal/cli/base.go, /main.go]
modify_files: [/main.go] 
create_files: [/internal/cli/cmd_prompt.go, /internal/cli/prompt_test.go]
```

**Existing Patterns to Follow**:
- `/internal/cli/tool.go:64-80` - CLI command structure and subcommand organization
- `/internal/cli/tool.go:157-170` - JSON output format handling
- `/internal/cli/base.go:59-76` - Output format support and error handling

**Dependencies Context**:
- `github.com/spf13/cobra` - CLI framework for command structure
- Existing BaseCommand with JSON output support

## Task Scope Boundaries

**MODIFY Zone** (Direct Changes):
```yaml
primary_files:
  - /internal/cli/cmd_prompt.go           # New prompt command implementation
  - /main.go                              # Register new prompt command

create_files:
  - /internal/cli/cmd_prompt.go           # Complete prompt CLI implementation
  - /internal/cli/prompt_test.go          # Unit tests for prompt commands
```

**REVIEW Zone** (Check for Impact):
```yaml
check_integration:
  - /internal/cli/base.go                 # Verify BaseCommand compatibility
  - /internal/cli/tool.go                 # Ensure pattern consistency
```

**IGNORE Zone** (Do Not Touch):
```yaml
ignore_completely:
  - /internal/tui/screens/*               # TUI layer
  - /internal/mcp/service.go              # Service layer (already has prompt methods)
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
- [Cobra CLI Framework](https://cobra.dev/) - Command structure and flag handling
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-06-18) - Prompts section 4.2 - Command naming and operation semantics

**Standards Applied**:
- [CLI Application Standards](https://www.gnu.org/prep/standards/standards.html#Command_002dLine-Interfaces) - Consistent flag patterns and help text
- Project JSON output format - Maintains consistency with tool commands

**Reference Implementation**:
- Tool CLI commands (tool.go) - Complete pattern to replicate - Command structure, error handling, JSON output

## Task Requirements

**Objective**: Create complete prompt CLI commands following exact tool CLI pattern

**Success Criteria**:
- [ ] PromptCommand struct following ToolCommand pattern (tool.go:15-18)
- [ ] Command structure with list/get/execute subcommands
- [ ] JSON output support for all subcommands (--output flag)
- [ ] Argument parsing for prompt execution (key=value pairs)
- [ ] Error handling following BaseCommand.HandleError pattern
- [ ] Progress messages suppressed in JSON mode (tool.go:140-154 pattern)
- [ ] Main.go registration following createToolCommand pattern
- [ ] Unit tests covering all command operations

**Validation Commands**:
```bash
# Command Registration Verification  
./mcp-tui prompt --help                                    # Command exists and shows help
./mcp-tui prompt list --help                               # Subcommands exist
./mcp-tui prompt list --output json | jq .                # JSON output works
go test internal/cli/prompt_test.go                        # Tests pass
```

## Risk Mitigation (from master analysis)

**High-Risk Mitigations**:
- None identified - following proven CLI pattern reduces all risks

**Context Validation**:
- [ ] Tool command pattern from tool.go successfully adapted
- [ ] JSON output format matches existing tool commands  
- [ ] BaseCommand integration maintains consistency

## Integration with Other Tasks

**Dependencies**: Task 01 (GetResource method) - not required for prompts
**Integration Points**: Uses existing service ListPrompts/GetPrompt methods
**Shared Context**: CLI pattern shared with resource CLI task

## Documentation from Master Context

### Implementation Pattern Reference
From `/context/implementation-patterns.md` - CLI Command Structure:

```go
// Complete command structure following tool.go pattern
type PromptCommand struct {
    *BaseCommand
}

func NewPromptCommand() *PromptCommand {
    return &PromptCommand{
        BaseCommand: NewBaseCommand(),
    }
}

func (pc *PromptCommand) CreateCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "prompt",
        Short: "Interact with MCP server prompts",
        Long:  "List, describe, and execute prompts provided by the MCP server",
    }
    
    // Add output format flag to all subcommands
    cmd.PersistentFlags().StringP("output", "o", "text", "Output format (text, json)")
    
    // Add subcommands
    cmd.AddCommand(pc.createListCommand())
    cmd.AddCommand(pc.createGetCommand())
    cmd.AddCommand(pc.createExecuteCommand())
    
    return cmd
}
```

### Command Structure Required
```bash
mcp-tui prompt list [--output json]                      # List all prompts
mcp-tui prompt get <name> [--output json]               # Get prompt details  
mcp-tui prompt execute <name> [arguments...] [--output json]  # Execute prompt
```

## Execution Notes
- **Start Pattern**: Copy entire tool.go structure and adapt for prompts
- **Key Context**: Maintain exact JSON output format and error handling
- **Integration Test**: Verify commands work with existing service methods
- **Review Focus**: Ensure command help text and examples are clear and consistent