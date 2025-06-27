#!/usr/bin/env node

// MCP server that violates protocol structure
const readline = require('readline');

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout
});

rl.on('line', (line) => {
  try {
    const message = JSON.parse(line);
    
    if (message.method === 'initialize') {
      // Missing required fields in response
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: {
          // Missing protocolVersion
          capabilities: {}
        }
      }));
    } else if (message.method === 'initialized') {
      // Wrong jsonrpc version
      console.log(JSON.stringify({
        jsonrpc: "1.0",
        result: {}
      }));
    } else if (message.method === 'tools/list') {
      // Wrong result structure - tools should be an array
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: {
          tools: "not an array"
        }
      }));
    } else if (message.method === 'resources/list') {
      // Missing id field for request that requires response
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        result: {
          resources: []
        }
      }));
    } else if (message.method === 'tools/call') {
      // Send notification instead of response
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        method: "notification/toolCalled",
        params: {}
      }));
    } else {
      // Send both result and error (protocol violation)
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: {},
        error: {
          code: -32000,
          message: "This violates protocol"
        }
      }));
    }
  } catch (e) {
    // Send response without request
    console.log(JSON.stringify({
      jsonrpc: "2.0",
      id: 99999,
      result: {
        message: "Unsolicited response"
      }
    }));
  }
});

process.on('SIGTERM', () => process.exit(0));
process.on('SIGINT', () => process.exit(0));