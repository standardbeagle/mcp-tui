// The stage every demo records on: a browser terminal (ttyd) plus the demo
// servers its scenes connect to. The demo engine spawns this as a demo's
// setup.upstream, waits for the terminal URL, and SIGTERMs it afterwards.
//
// Each page load gets a fresh bash in docs/recordings/workspace with
// docs/recordings/.bin (mcp-tui, demo-server; `make demo-bins`) and the
// pinned node servers first on PATH. Every listener is loopback only.
//
//   http://127.0.0.1:7690/         terminal
//   http://127.0.0.1:8931/mcp      demo server, streamable HTTP
//   http://127.0.0.1:8932/sse      demo server, legacy SSE
//   http://127.0.0.1:8933/mcp      demo server behind OAuth (-oauth)
//   http://127.0.0.1:8934/mcp      demo server breaking verify's rules (-misbehave)
import {spawn} from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const bin = path.join(here, '.bin');
const workspace = path.join(here, 'workspace');
const demoServer = path.join(bin, 'demo-server');
if (!fs.existsSync(demoServer) || !fs.existsSync(path.join(bin, 'mcp-tui'))) {
  console.error(`stage: ${bin} lacks mcp-tui or demo-server; run make demo-bins`);
  process.exit(1);
}
fs.mkdirSync(workspace, {recursive: true});

// Catppuccin Mocha, the palette the earlier VHS recordings used.
const theme = {
  background: '#1e1e2e', foreground: '#cdd6f4', cursor: '#f5e0dc', selectionBackground: '#585b70',
  black: '#45475a', red: '#f38ba8', green: '#a6e3a1', yellow: '#f9e2af', blue: '#89b4fa',
  magenta: '#f5c2e7', cyan: '#94e2d5', white: '#bac2de', brightBlack: '#585b70', brightRed: '#f38ba8',
  brightGreen: '#a6e3a1', brightYellow: '#f9e2af', brightBlue: '#89b4fa', brightMagenta: '#f5c2e7',
  brightCyan: '#94e2d5', brightWhite: '#a6adc8',
};

const env = {
  ...process.env,
  PATH: [bin, path.join(here, 'node_modules/.bin'), process.env.PATH].join(path.delimiter),
  PS1: '\\[\\e[1;35m\\]❯\\[\\e[0m\\] ',
  TERM: 'xterm-256color',
  // Scenes show mcp-tui's own output, not a stale cached OAuth token.
  XDG_CACHE_HOME: path.join(workspace, '.cache'),
};

const children = [];
const start = (cmd, args, name) => {
  const child = spawn(cmd, args, {cwd: workspace, env, stdio: ['ignore', 'ignore', 'pipe']});
  child.stderr.on('data', (b) => process.stderr.write(`[${name}] ${b}`));
  child.on('exit', (code, signal) => {
    if (!stopping) {
      console.error(`stage: ${name} exited (${signal ?? code})`);
      stop(1);
    }
  });
  children.push(child);
};

let stopping = false;
const stop = (code = 0) => {
  stopping = true;
  for (const c of children) c.kill('SIGTERM');
  process.exit(code);
};
process.on('SIGTERM', () => stop(0));
process.on('SIGINT', () => stop(0));

start(demoServer, ['-http', '127.0.0.1:8931', '-sse', '127.0.0.1:8932'], 'desk');
start(demoServer, ['-http', '127.0.0.1:8933', '-oauth'], 'desk-oauth');
start(demoServer, ['-http', '127.0.0.1:8934', '-misbehave'], 'desk-misbehave');
start('ttyd', [
  '--port', '7690', '--interface', '127.0.0.1', '--writable',
  '-t', 'fontSize=16', '-t', 'fontFamily=JetBrains Mono', '-t', 'lineHeight=1.15',
  '-t', `theme=${JSON.stringify(theme)}`, '-t', 'disableLeaveAlert=true', '-t', 'disableResizeOverlay=true',
  'bash', '--noprofile', '--norc', '-i',
], 'ttyd');
