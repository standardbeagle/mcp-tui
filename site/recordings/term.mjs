// Drives the stage's browser terminal from a demo scene (an engine `browser`
// segment with raw:true and url http://127.0.0.1:7690/). Typing goes through
// the page's keyboard into xterm, and waits read xterm's screen buffer, so a
// scene waits for what is on screen rather than for a guessed delay.
//
//   import {terminal} from '../../term.mjs';
//   export default async function run(d) {
//     const t = await terminal(d);
//     await t.run('mcp-tui --cmd demo-server --args -stdio tool list');
//     await t.waitFor(/Total: 7 tools/);
//   }

// The window around the terminal: padding, rounded corners, a title bar.
const frameCSS = `
  html, body { background: #11111b !important; margin: 0; height: 100%; }
  body { padding: 28px 32px 32px; box-sizing: border-box; }
  #terminal-container { height: calc(100% - 34px) !important; width: 100% !important;
    border-radius: 0 0 12px 12px; overflow: hidden; background: #1e1e2e; padding: 10px 14px; box-sizing: border-box; }
  body::before { content: ''; display: block; height: 34px; border-radius: 12px 12px 0 0; background: #181825;
    background-image: radial-gradient(circle at 20px 17px, #f38ba8 6px, transparent 7px),
      radial-gradient(circle at 42px 17px, #f9e2af 6px, transparent 7px),
      radial-gradient(circle at 64px 17px, #a6e3a1 6px, transparent 7px); }
`;

const screenText = () => {
  const buf = window.term.buffer.active;
  const lines = [];
  for (let i = 0; i < buf.length; i++) lines.push(buf.getLine(i)?.translateToString(true) ?? '');
  return lines.join('\n');
};

/**
 * setup: shell commands run before the 'ready' mark (cut by the demo's keep
 * range), e.g. removing files an earlier take left in the workspace.
 */
export async function terminal(d, {typingDelay = 38, setup = []} = {}) {
  const page = d.page;
  await page.waitForFunction(() => window.term && window.term.buffer, undefined, {timeout: 20000});
  await page.addStyleTag({content: frameCSS});
  await page.evaluate(() => window.dispatchEvent(new Event('resize')));
  await page.locator('.xterm-helper-textarea').focus();
  await waitForPrompt(page, 10000);
  for (const cmd of setup) {
    await page.keyboard.type(cmd);
    await page.keyboard.press('Enter');
    await waitForPrompt(page, 30000);
  }
  if (setup.length > 0) {
    await page.keyboard.press('Control+l');
    await page.evaluate(() => window.term.clear());
    await waitForPrompt(page, 5000);
  }
  d.mark('ready');

  const t = {
    page,
    /** Type text as a person would; no Enter. */
    type: (text, delay = typingDelay) => page.keyboard.type(text, {delay}),
    /** Press one key, e.g. 'Enter', 'Tab', 'ArrowDown', 'Control+d'. */
    press: async (key, times = 1) => {
      for (let i = 0; i < times; i++) {
        await page.keyboard.press(key);
        if (times > 1) await page.waitForTimeout(140);
      }
    },
    /** Type a command, pause as a reader would, press Enter. */
    run: async (cmd) => {
      await t.type(cmd);
      await page.waitForTimeout(450);
      await page.keyboard.press('Enter');
    },
    /** Wait until the screen shows re (the whole scrollback is searched). */
    waitFor: async (re, timeout = 30000) => {
      const deadline = Date.now() + timeout;
      for (;;) {
        const text = await page.evaluate(screenText);
        if (re.test(text)) return text;
        if (Date.now() > deadline) {
          throw new Error(`terminal never showed ${re} within ${timeout}ms; screen ends:\n${text.trimEnd().split('\n').slice(-15).join('\n')}`);
        }
        await page.waitForTimeout(100);
      }
    },
    /** Wait for the shell prompt after the last command finished. */
    prompt: (timeout = 30000) => waitForPrompt(page, timeout),
    /** Hold the frame so a viewer can read it. */
    hold: (ms) => page.waitForTimeout(ms),
    /** Leave the TUI: Esc backs out one screen at a time and quits from the
     *  main screen, so press it until the shell prompt is back. */
    quitTUI: async () => {
      for (let i = 0; i < 6; i++) {
        await page.keyboard.press('Escape');
        try {
          await waitForPrompt(page, 1200);
          return;
        } catch {
          // still inside the TUI
        }
      }
      throw new Error('the TUI did not exit after 6 presses of Esc');
    },
    /** Clear the screen without showing the command. */
    clear: async () => {
      await page.evaluate(() => window.term.clear());
      await page.keyboard.press('Control+l');
      await page.waitForTimeout(150);
    },
  };
  return t;
}

// The prompt is the last non-empty line ending in the ❯ marker.
async function waitForPrompt(page, timeout) {
  await page.waitForFunction(() => {
    const buf = window.term.buffer.active;
    for (let i = buf.baseY + buf.cursorY; i >= 0; i--) {
      const line = buf.getLine(i)?.translateToString(true).trimEnd() ?? '';
      if (line !== '') return line === '❯';
    }
    return false;
  }, undefined, {timeout});
}
