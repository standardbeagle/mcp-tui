// Names, titles and annotation markers; the full definition of one tool;
// and the confirmation mcp-tui asks before a destructive tool runs.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('export DESK=http://127.0.0.1:8931/mcp');
  await t.prompt();
  await t.run('mcp-tui --url $DESK tool list 2>/dev/null');
  await t.waitFor(/open world[\s\S]*❯\s*$/);
  await t.hold(5000);
  await t.clear();
  await t.run('mcp-tui --url $DESK tool describe search_tickets 2>/dev/null | head -30');
  await t.waitFor(/Input Schema[\s\S]*❯\s*$/);
  await t.hold(5000);
  await t.clear();
  await t.run('mcp-tui --url $DESK tool call delete_ticket ticket_id=T-1047');
  await t.waitFor(/Proceed\? \[y\/N\]:/);
  await t.hold(2500);
  await t.type('n');
  await t.press('Enter');
  await t.waitFor(/cancelled[\s\S]*❯\s*$/);
  await t.hold(3500);
}
