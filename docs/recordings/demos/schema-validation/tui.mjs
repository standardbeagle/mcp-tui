// The TUI form refuses the same call, then Ctrl+O sends it anyway.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('mcp-tui --url http://127.0.0.1:8931/mcp');
  await t.waitFor(/Search tickets/);
  await t.press('ArrowDown', 6);
  await t.press('Enter');
  await t.waitFor(/Execute Tool: Search tickets/);
  await t.hold(1200);
  await t.press('Tab');
  await t.type('500');
  await t.hold(600);
  await t.press('Enter');
  await t.waitFor(/maximum 50/);
  await t.hold(4000);
  await t.press('Control+o');
  await t.waitFor(/schema violations sent/);
  await t.hold(1500);
  await t.press('Enter');
  await t.waitFor(/isError|error/i);
  await t.hold(4500);
  await t.quitTUI();
}
