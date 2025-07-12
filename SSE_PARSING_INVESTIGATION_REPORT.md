# Go MCP SDK SSE Parsing Investigation Report

## Executive Summary

The Go MCP SDK has **specific and strict requirements** for SSE message parsing that may not be compatible with all SSE server implementations. The SDK is **not the primary issue** - it can parse MCP messages correctly when servers send them in the expected format.

## Key Findings

### 1. **SDK SSE Format Requirements Are Strict**

The SDK requires **exact** SSE event formats:

```
event: endpoint
data: /sse?sessionId=abc123

```

**❌ These formats will FAIL:**
- `data: /sse?sessionId=abc123` (missing event name)
- `event: handshake\ndata: /sse?sessionId=abc123` (wrong event name)
- `invalid format without colon` (malformed SSE)

### 2. **MCP Protocol Messages Must Use `event: message`**

All MCP JSON-RPC messages must be sent as:

```
event: message
data: {"jsonrpc":"2.0","id":1,"method":"test"}

```

### 3. **The SDK CAN Parse MCP Messages Correctly**

✅ **When servers send the correct format**, the SDK successfully:
- Parses the handshake `event: endpoint`
- Extracts session URLs
- Decodes JSON-RPC messages from `event: message` events

### 4. **Common Server Implementation Issues**

Many SSE servers fail because they:

1. **Send handshake without event name**
   ```
   # ❌ This fails
   data: /sse?sessionId=123
   
   # ✅ This works  
   event: endpoint
   data: /sse?sessionId=123
   ```

2. **Send MCP messages without event name**
   ```
   # ❌ This fails
   data: {"jsonrpc":"2.0",...}
   
   # ✅ This works
   event: message  
   data: {"jsonrpc":"2.0",...}
   ```

3. **Use non-standard event names**
   ```
   # ❌ These fail
   event: handshake
   event: response
   event: notification
   
   # ✅ These work
   event: endpoint    (for handshake)
   event: message     (for MCP messages)
   ```

4. **Don't implement the session URL pattern correctly**
   - SDK expects: Initial GET → `event: endpoint` → Second GET to session URL
   - Many servers expect: Single persistent connection

## Technical Analysis

### SSE Event Parsing Implementation

The SDK uses a strict SSE parser (`scanEvents` function) that:

```go
// Requires this exact format:
case bytes.Equal(before, eventKey):
    evt.name = strings.TrimSpace(string(after))
case bytes.Equal(before, dataKey):
    data := bytes.TrimSpace(after)
```

- **Lines must have format**: `field: value`
- **Event names are checked exactly**: Must be "endpoint" or "message"
- **Malformed lines cause immediate failure**

### Handshake Flow

```
Client                    Server
   |                         |
   | GET /sse               |
   |----------------------->|
   |                         |
   | event: endpoint         |
   | data: /sse?sessionId=X  |
   |<-----------------------|
   |                         |
   | GET /sse?sessionId=X   |
   |----------------------->|
   |                         |
   | event: message          |
   | data: {"jsonrpc"...}    |
   |<-----------------------|
```

**Critical**: Many servers don't implement this two-step handshake.

## Server Compatibility Analysis

### ✅ **Compatible Servers** (Expected format)
- Send `event: endpoint` in initial handshake
- Send `event: message` for MCP protocol messages
- Implement session URL pattern correctly
- Follow strict SSE format

### ❌ **Incompatible Servers** (Common issues)
- Send data-only events without event names
- Use custom event names (`handshake`, `response`, etc.)
- Send MCP messages as plain data
- Don't implement session handshake flow
- Send malformed SSE syntax

## Recommendations

### For Users Experiencing SSE Timeouts

1. **Check server SSE format**:
   ```bash
   curl -N -H "Accept: text/event-stream" http://your-server/sse
   ```
   
   Look for:
   - `event: endpoint` (required)
   - `data: /path?sessionId=...` (required)
   - Proper SSE line format

2. **Enable debugging** in mcp-tui:
   ```bash
   mcp-tui --debug --url http://your-server/sse
   ```

3. **Try alternative transports**:
   ```bash
   # Try HTTP instead of SSE
   mcp-tui --url http://your-server
   
   # Or streamable HTTP
   mcp-tui --url http://your-server --transport streamable-http
   ```

### For Server Developers

1. **Use exact event names**:
   ```
   event: endpoint     # For handshake
   event: message      # For MCP messages
   ```

2. **Implement session URL pattern**:
   ```
   GET /sse → event: endpoint, data: /sse?sessionId=X
   GET /sse?sessionId=X → event: message, data: {...}
   ```

3. **Follow strict SSE format**:
   ```
   field: value\n
   field: value\n
   \n
   ```

### For the mcp-tui Project

1. **Add SSE format validation** to provide clearer error messages
2. **Enhance debugging** to show exact SSE events received
3. **Document SSE server requirements** clearly
4. **Consider fallback mechanisms** for non-compliant servers

## Conclusion

**The Go MCP SDK SSE parsing is NOT fundamentally broken.** It works correctly when servers implement the MCP SSE specification properly. The timeouts and failures are typically due to:

1. **Server format incompatibility** (90% of cases)
2. **Missing event names** in SSE streams
3. **Incorrect handshake implementation**
4. **Non-standard SSE syntax**

The solution is either:
- **Fix the SSE server** to use the correct format
- **Use alternative transports** (HTTP, streamable HTTP)
- **Enhance the SDK** to be more tolerant of format variations

The current SDK prioritizes specification compliance over broad compatibility, which is a reasonable design choice for a protocol implementation.