# Array Field Behavior in MCP-TUI

## Issue Description

When executing a tool with array parameters, you may observe different behavior between the first execution and subsequent executions:

- **First execution**: Shows an empty array `[]` or omits the field entirely
- **Subsequent executions**: Shows the actual array values

## Root Causes

### 1. Empty Array Handling
Previously, when a user left an array field empty:
- **Optional fields**: Were omitted from the request entirely
- **Required fields**: Were also omitted, potentially causing server errors
- **Comma-only input** (e.g., just typing `,`): Created `["", ""]` (array with empty strings)

### 2. Server-Side State
Some MCP servers may:
- Initialize arrays differently on first call vs subsequent calls
- Maintain session state between calls
- Apply different defaults when a field is missing vs when it's explicitly `[]`

## The Fix

The tool screen now handles array fields more consistently:

### Required Array Fields
- Empty input → Sends `[]` (empty array)
- Ensures the field is always included in the request

### Optional Array Fields  
- Empty input → Field is omitted (not sent)
- Explicit `[]` → Sends empty array
- Comma-separated values → Filters out empty strings

### Examples

| User Input | Required Field | Optional Field |
|------------|----------------|----------------|
| (empty)    | `[]`           | (omitted)      |
| `[]`       | `[]`           | `[]`           |
| `a,b,c`    | `["a","b","c"]`| `["a","b","c"]`|
| `a,b,`     | `["a","b"]`    | `["a","b"]`    |
| `,`        | `[]`           | `[]`           |
| `,,a,,`    | `["a"]`        | `["a"]`        |

## Testing Your Specific Case

To determine if this was the issue you encountered:

1. Check if the problematic field is an array type
2. Note whether it's marked as required in the tool schema
3. Observe what you entered (or didn't enter) in the field
4. Compare the server's response on first vs subsequent executions

## Workarounds (if not yet updated)

If you're experiencing this issue:
1. For required arrays: Enter `[]` explicitly instead of leaving blank
2. For optional arrays you want empty: Enter `[]` 
3. For optional arrays you want omitted: Leave blank
4. Avoid trailing commas in comma-separated lists

## Server-Side Considerations

MCP server developers should:
- Handle missing array parameters consistently
- Distinguish between missing fields and empty arrays
- Document default behavior for array parameters
- Avoid stateful behavior that changes between calls