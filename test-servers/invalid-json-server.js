#!/usr/bin/env node

// MCP server that sends invalid JSON responses
const readline = require('readline');

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout
});

rl.on('line', (line) => {
  try {
    const message = JSON.parse(line);
    
    if (message.method === 'initialize') {
      // Send back invalid JSON - missing closing brace
      console.log('{"jsonrpc": "2.0", "id": ' + message.id + ', "result": {"protocolVersion": "2024-11-05", "capabilities": {}');
    } else if (message.method === 'initialized') {
      // Send malformed JSON with trailing comma
      console.log('{"jsonrpc": "2.0", "result": {"status": "ok",}, "id": null}');
    } else if (message.method === 'tools/list') {
      // Send JSON with syntax error - unquoted key
      console.log('{jsonrpc: "2.0", "id": ' + message.id + ', "result": {"tools": []}}');
    } else if (message.method === 'resources/list') {
      // Send completely broken JSON
      console.log('This is not JSON at all! {broken: syntax');
    } else {
      // Send JSON with undefined values (invalid in JSON)
      console.log('{"jsonrpc": "2.0", "id": ' + message.id + ', "result": undefined}');
    }
  } catch (e) {
    // If we can't parse input, send garbage
    console.log('GARBAGE OUTPUT: ' + Math.random());
  }
});

process.on('SIGTERM', () => process.exit(0));
process.on('SIGINT', () => process.exit(0));