// Run a tool, then walk the debug screen's tabs: messages, a message in
// full, HTTP timing, capabilities, notifications.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('mcp-tui --url http://127.0.0.1:8931/mcp --server-log-level info');
  await t.waitFor(/Search tickets/);
  await t.press('ArrowDown', 3);           // escalate_ticket: progress + logs
  await t.press('Enter');
  await t.waitFor(/Execute Tool: Escalate ticket/);
  await t.press('Tab');                    // reason is first; ticket_id next
  await t.type('T-1041');
  await t.press('Enter');
  await t.waitFor(/Escalated T-1041/, 30000);
  await t.hold(2000);
  await t.press('Control+d');
  await t.waitFor(/MCP Protocol/);
  await t.hold(2000);
  for (const tab of ['MCP Protocol', 'HTTP Debug', 'Auth', 'Statistics', 'Capabilities', 'Notifications']) {
    await t.press('Tab');
    await t.hold(tab === 'MCP Protocol' || tab === 'HTTP Debug' ? 3500 : 2500);
    if (tab === 'MCP Protocol' || tab === 'HTTP Debug') {
      await t.press('Enter');
      await t.hold(3500);
      await t.press('b');                  // back to the list
      await t.hold(600);
    }
  }
  await t.quitTUI();
}
