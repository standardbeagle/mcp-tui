# Demo recordings

The videos on the docs site and the loops in the README are recorded from
real mcp-tui runs against real servers, scripted so every take is the same.

```bash
make demo NAME=verify-a-tool   # record one demo and publish it into docs/
make demos                     # record and publish every demo
```

`make demo` builds `mcp-tui` and the demo server into `.bin/`, installs the
pinned node servers (`package.json`), records the demo with the agnt demo
engine, and runs `publish.mjs`, which writes `docs/public/videos/<name>.*`
and, for demos marked `readmeLoop`, the animated loop in
`docs/src/assets/recordings/`. The engine lives in the agnt repo
(`~/work/core/agnt/docs-site/screenshots/engine/demo.mjs`; override with
`DEMO_ENGINE=`). It needs ffmpeg and Playwright's Chromium; narrated demos
also need `edge-tts`.

## Pieces

| File | Role |
|---|---|
| `demos/<name>/demo.json` | the demo: title cards, scenes, logo overlay |
| `demos/<name>/scene.mjs` | a scene: what is typed and what is waited for on screen |
| `stage.mjs` | started by each demo: a browser terminal (ttyd, port 7690) and the demo servers on 8931-8934 |
| `term.mjs` | the scene driver: `run`, `type`, `press`, `waitFor(/regex/)`, `prompt`, `hold` |
| `publish.mjs` | engine output → site files; fills duration and date in `docs/src/data/videos.json` |
| `demo-server/` | the Acme support desk MCP server every demo talks to |
| `assets/sb-logo-on-dark.png` | the Standard Beagle logo, wordmark lightened for dark frames, which the engine overlays top right |

Every video needs an entry in `docs/src/data/videos.json` (title,
description) before it is published; the docs embed it with
`<DemoVideo name="…" />`, which also emits the schema.org `VideoObject`.

## Why scenes drive a browser terminal and not VHS

The scenes type into ttyd's xterm through Playwright and wait on xterm's
screen buffer, so a step waits for what is on screen rather than for a
guessed delay, and a loaded machine makes a take slower, never wrong. VHS
did the same job before, but its bundled browser launcher hangs on this
setup before the first keystroke, while Playwright's Chromium reaches a
ready terminal in under two seconds.

## Writing a scene

```js
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);          // waits for the prompt, marks 'ready'
  await t.run('mcp-tui --url http://127.0.0.1:8931/mcp tool list');
  await t.waitFor(/Total: 7 tools/);
  await t.hold(2500);                   // let the viewer read it
}
```

Give the scene segment `"keep": [["mark:ready", "end"]]` so the page load is
cut. The workspace the shell starts in (`workspace/`) is scratch and not
committed.
