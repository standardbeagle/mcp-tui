// Run a tool by hand in the TUI, export the session with Ctrl+E, and
// replay the generated script.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d, {setup: ['rm -f mcp-tui-session-*']});
  await t.run('mcp-tui --url http://127.0.0.1:8931/mcp');
  await t.waitFor(/Search tickets/);
  await t.hold(1000);
  await t.press('ArrowDown', 6);
  await t.press('Enter');
  await t.waitFor(/Execute Tool: Search tickets/);
  await t.press('Tab');
  await t.type('3');
  await t.press('Tab', 2);
  await t.type('open');
  await t.press('Enter');
  await t.waitFor(/T-10\d\d/);
  await t.hold(2500);
  await t.press('Escape');
  await t.hold(600);
  await t.press('Control+e');
  await t.waitFor(/Exported session to/);
  await t.hold(3000);
  await t.quitTUI();
  await t.run('cat mcp-tui-session-*.sh');
  await t.waitFor(/tool call search_tickets[\s\S]*❯\s*$/);
  await t.hold(4000);
  await t.run('./mcp-tui-session-*.sh 2>/dev/null | head -8');
  await t.waitFor(/T-10\d\d[\s\S]*❯\s*$/);
  await t.hold(3500);
}
