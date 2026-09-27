// The same two requests answered from stub flags.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('export DESK=http://127.0.0.1:8931/mcp');
  await t.prompt();
  await t.run(`mcp-tui --url $DESK tool call schedule_callback ticket_id=T-1041 --elicit-stub '{"time":"2026-10-02T15:00:00Z"}' 2>/dev/null`);
  await t.waitFor(/Input rounds[\s\S]*❯\s*$/);
  await t.hold(4500);
  await t.clear();
  await t.run(`mcp-tui --url $DESK tool call draft_reply ticket_id=T-1042 --sampling-stub "Thanks, the fix ships today." 2>/dev/null`);
  await t.waitFor(/Served by[\s\S]*❯\s*$/);
  await t.hold(4500);
}
