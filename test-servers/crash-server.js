#!/usr/bin/env node

// MCP server that crashes at various points
const readline = require('readline');

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout
});

let messageCount = 0;

rl.on('line', (line) => {
  messageCount++;
  
  try {
    const message = JSON.parse(line);
    
    if (message.method === 'initialize') {
      // Crash immediately after receiving initialize
      console.log(JSON.stringify({
        jsonrpc: "2.0",
        id: message.id,
        result: {
          protocolVersion: "2024-11-05",
          capabilities: {}
        }
      }));
      
      // Simulate different crash scenarios
      setTimeout(() => {
        if (Math.random() > 0.5) {
          // Throw unhandled exception
          throw new Error("CRASH: Unhandled server error!");
        } else {
          // Exit abruptly
          process.exit(1);
        }
      }, 100);
      
    } else if (message.method === 'initialized') {
      // Crash during initialized
      process.stderr.write("FATAL: Server encountered critical error\n");
      process.exit(2);
      
    } else if (message.method === 'tools/list') {
      // Send partial response then crash
      process.stdout.write('{"jsonrpc": "2.0", "id": ' + message.id + ', "result": {"tools": [');
      // Crash mid-response
      process.exit(3);
      
    } else {
      // Random crashes for other methods
      if (messageCount > 3) {
        process.stderr.write("Segmentation fault (core dumped)\n");
        process.exit(139); // Typical segfault exit code
      }
    }
  } catch (e) {
    // Crash on parse error
    process.stderr.write("PANIC: " + e.message + "\n");
    process.exit(255);
  }
});

// Also crash on various signals
process.on('SIGTERM', () => {
  process.stderr.write("SIGTERM received, crashing...\n");
  process.exit(128 + 15);
});

// Simulate random crashes
setInterval(() => {
  if (Math.random() < 0.1) {
    process.stderr.write("Random crash triggered\n");
    process.abort(); // This will cause SIGABRT
  }
}, 5000);