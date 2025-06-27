#!/usr/bin/env node

// MCP server that sends messages out of order
const readline = require('readline');

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout
});

const pendingRequests = [];
let initialized = false;

rl.on('line', (line) => {
  try {
    const message = JSON.parse(line);
    
    if (message.method === 'initialize') {
      // Send initialized notification before initialize response
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        method: "initialized",
        params: {}
      }));
      
      // Then send the actual response
      setTimeout(() => {
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
      }, 100);
      
    } else if (message.method === 'initialized') {
      initialized = true;
      // Send responses to any pending requests that came before initialized
      pendingRequests.forEach(req => {
        console.log(JSON.stringify({
          jsonrpc: "2.0",
          id: req.id,
          result: {}
        }));
      });
      
    } else if (message.method === 'tools/list') {
      // Send response with wrong ID first
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id + 1000,
        result: {
          tools: []
        }
      }));
      
      // Then send correct response
      setTimeout(() => {
        console.log(JSON.stringify({
          jsonrpc: "2.0",
          id: message.id,
          result: {
            tools: [
              {
                name: "outOfOrderTool",
                description: "This tool's response might come out of order",
                inputSchema: { type: "object" }
              }
            ]
          }
        }));
      }, 200);
      
    } else if (message.method === 'resources/list') {
      // Queue up multiple responses
      if (!initialized) {
        pendingRequests.push(message);
      }
      
      // Send responses in reverse order
      const responses = [];
      for (let i = 0; i < 3; i++) {
        responses.push({
          jsonrpc: "2.0",
          id: message.id,
          result: {
            resources: [{
              uri: `resource://item${i}`,
              name: `Resource ${i}`
            }]
          }
        });
      }
      
      responses.reverse().forEach((resp, index) => {
        setTimeout(() => {
          console.log(JSON.stringify(resp));
        }, index * 50);
      });
      
    } else if (message.method === 'tools/call') {
      // Send progress notifications after the final response
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: {
          content: [{
            type: "text",
            text: "Tool completed"
          }]
        }
      }));
      
      // Send progress updates after completion
      setTimeout(() => {
        console.log(JSON.stringify({
          jsonrpc: "2.0",
          method: "notifications/progress",
          params: {
            progressToken: message.params?.progressToken,
            progress: 50,
            total: 100
          }
        }));
      }, 100);
      
    } else if (message.method === 'prompts/get') {
      // Send multiple responses for single request
      for (let i = 0; i < 3; i++) {
        console.log(JSON.stringify({
          jsonrpc: "2.0",
          id: message.id,
          result: {
            messages: [{
              role: "user",
              content: {
                type: "text",
                text: `Response ${i}`
              }
            }]
          }
        }));
      }
      
    } else {
      // Randomly delay responses
      const delay = Math.floor(Math.random() * 500);
      setTimeout(() => {
        console.log(JSON.stringify({
          jsonrpc: "2.0",
          id: message.id,
          result: {}
        }));
      }, delay);
      
      // Sometimes send duplicate responses
      if (Math.random() > 0.5) {
        setTimeout(() => {
          console.log(JSON.stringify({
            jsonrpc: "2.0",
            id: message.id,
            result: {
              duplicate: true
            }
          }));
        }, delay + 100);
      }
    }
  } catch (e) {
    // Send error response immediately, even if other responses are pending
    console.log(JSON.stringify({
      jsonrpc: "2.0",
      id: null,
      error: {
        code: -32700,
        message: "Parse error"
      }
    }));
  }
});

process.on('SIGTERM', () => process.exit(0));
process.on('SIGINT', () => process.exit(0));