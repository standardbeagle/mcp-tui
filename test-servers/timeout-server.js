#!/usr/bin/env node

// MCP server that hangs or times out
const readline = require('readline');

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout
});

rl.on('line', (line) => {
  try {
    const message = JSON.parse(line);
    
    if (message.method === 'initialize') {
      // Take very long to respond (30 seconds)
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
      }, 30000);
      
    } else if (message.method === 'initialized') {
      // Never respond to initialized
      // Just hang forever
      
    } else if (message.method === 'tools/list') {
      // Send response byte by byte very slowly
      const response = JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: {
          tools: [
            {
              name: "slowTool",
              description: "This tool is very slow",
              inputSchema: {
                type: "object",
                properties: {}
              }
            }
          ]
        }
      });
      
      // Send one character every 500ms
      let i = 0;
      const interval = setInterval(() => {
        if (i < response.length) {
          process.stdout.write(response[i]);
          i++;
        } else {
          process.stdout.write('\n');
          clearInterval(interval);
        }
      }, 500);
      
    } else if (message.method === 'resources/list') {
      // Start sending response but never complete it
      process.stdout.write('{"jsonrpc": "2.0", "id": ' + message.id + ', "result": {"resources": [');
      // Never close the JSON
      
    } else if (message.method === 'tools/call') {
      // Infinite loop - server becomes unresponsive
      while (true) {
        // Busy wait
      }
      
    } else {
      // Random delays between 5-20 seconds
      const delay = Math.floor(Math.random() * 15000) + 5000;
      setTimeout(() => {
        console.log(JSON.stringify({
          jsonrpc: "2.0",
          id: message.id,
          error: {
            code: -32000,
            message: "Sorry for the delay"
          }
        }));
      }, delay);
    }
  } catch (e) {
    // Even errors take forever
    setTimeout(() => {
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: null,
        error: {
          code: -32700,
          message: "Parse error (after timeout)"
        }
      }));
    }, 10000);
  }
});

// Don't respond to signals quickly either
process.on('SIGTERM', () => {
  setTimeout(() => process.exit(0), 5000);
});
process.on('SIGINT', () => {
  setTimeout(() => process.exit(0), 5000);
});