# Task: Add Connection Screen Return Functionality

## Request
Add a way to get back to the MCP connection screen from the main screen, particularly useful for handling typos in connection details.

## Implementation Plan

### [ ] 1. Add disconnect command to main screen key handler
- **Complexity**: Low
- **Location**: `/internal/tui/screens/main.go`
- **Details**: 
  - Add a new keyboard shortcut (suggested: `d` for disconnect or `c` for connect)
  - Handle the key in `handleKeyMsg` method
  - Trigger disconnect and transition back to connection screen

### [ ] 2. Implement disconnect logic in MCP service
- **Complexity**: Medium
- **Location**: `/internal/mcp/service.go`
- **Details**:
  - Add a `Disconnect()` method to the Service interface
  - Properly close MCP client connection
  - Clean up any resources (goroutines, channels, etc.)
  - Reset service state to allow reconnection

### [ ] 3. Handle screen transition from main to connection screen
- **Complexity**: Low
- **Location**: `/internal/tui/screens/main.go` and `/internal/tui/app/manager.go`
- **Details**:
  - Use `TransitionMsg` to navigate back to connection screen
  - Ensure connection screen properly resets its state
  - Clear any previous connection data

### [ ] 4. Update help text to include new disconnect command
- **Complexity**: Low
- **Location**: `/internal/tui/screens/main.go` (View method)
- **Details**:
  - Add the new keyboard shortcut to the help text
  - Update any documentation strings

### [ ] 5. Test the implementation
- **Complexity**: Medium
- **Scenarios to test**:
  - Connect successfully, then disconnect and reconnect
  - Disconnect while operations are in progress
  - Connect with typo, disconnect, fix typo, reconnect
  - Ensure no resource leaks when disconnecting
  - Test with different transport types (stdio, SSE, HTTP)

## Technical Considerations

1. **State Management**: Ensure the connection screen resets properly when returning from main screen
2. **Resource Cleanup**: Properly close all connections and clean up goroutines
3. **User Experience**: Show appropriate feedback during disconnect process
4. **Error Handling**: Handle any errors during disconnect gracefully

## Chosen Approach
- Use keyboard shortcut `d` (for disconnect) on the main screen
- Clean disconnect of MCP service before transitioning
- Return to connection screen with fresh state
- Preserve the ability to reconnect to the same or different server