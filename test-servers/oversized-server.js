#!/usr/bin/env node

// MCP server that sends oversized messages
const readline = require('readline');

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout
});

// Generate large strings
function generateLargeString(size) {
  return 'x'.repeat(size);
}

rl.on('line', (line) => {
  try {
    const message = JSON.parse(line);
    
    if (message.method === 'initialize') {
      // Send normal initialize response
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: {
          protocolVersion: "2024-11-05",
          capabilities: {
            tools: {},
            resources: {}
          }
        }
      }));
      
    } else if (message.method === 'tools/list') {
      // Send massive tool list
      const tools = [];
      for (let i = 0; i < 1000; i++) {
        tools.push({
          name: `tool_${i}`,
          description: generateLargeString(10000), // 10KB description each
          inputSchema: {
            type: "object",
            properties: {
              data: {
                type: "string",
                description: generateLargeString(5000)
              }
            }
          }
        });
      }
      
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: { tools }
      }));
      
    } else if (message.method === 'resources/list') {
      // Send resources with huge URIs
      const resources = [];
      for (let i = 0; i < 100; i++) {
        resources.push({
          uri: `resource://path/${generateLargeString(50000)}/${i}`,
          name: `Resource ${i}`,
          description: generateLargeString(100000) // 100KB description
        });
      }
      
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: { resources }
      }));
      
    } else if (message.method === 'tools/call') {
      // Send enormous tool result
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: {
          content: [
            {
              type: "text",
              text: generateLargeString(10 * 1024 * 1024) // 10MB response
            }
          ]
        }
      }));
      
    } else if (message.method === 'resources/read') {
      // Send multiple huge content items
      const contents = [];
      for (let i = 0; i < 10; i++) {
        contents.push({
          uri: message.params?.uri || "resource://test",
          mimeType: "text/plain",
          text: generateLargeString(5 * 1024 * 1024) // 5MB each
        });
      }
      
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: { contents }
      }));
      
    } else {
      // Send response with deeply nested structure
      let nested = { value: "bottom" };
      for (let i = 0; i < 10000; i++) {
        nested = { level: i, data: nested, padding: generateLargeString(100) };
      }
      
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: nested
      }));
    }
  } catch (e) {
    // Even error messages are huge
    console.log(JSON.stringify({
      jsonrpc: "2.0",
      id: null,
      error: {
        code: -32700,
        message: "Parse error: " + generateLargeString(1024 * 1024),
        data: {
          stackTrace: generateLargeString(500000),
          context: generateLargeString(500000)
        }
      }
    }));
  }
});

process.on('SIGTERM', () => process.exit(0));
process.on('SIGINT', () => process.exit(0));