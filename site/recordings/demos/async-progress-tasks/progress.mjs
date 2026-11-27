// A long tool call: the progress line and the server's log lines arrive
// while it runs.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('export DESK=http://127.0.0.1:8931/mcp');
  await t.prompt();
  await t.run('mcp-tui --url $DESK --server-log-level info tool call escalate_ticket ticket_id=T-1041');
  await t.waitFor(/Served by[\s\S]*❯\s*$/, 40000);
  await t.hold(4000);
}
