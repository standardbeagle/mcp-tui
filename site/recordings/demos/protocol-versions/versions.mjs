// The negotiated version per server and transport, a pinned older version,
// and the input rounds a 2026-07-28 call took to get an elicitation answer.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('export DESK=http://127.0.0.1:8931/mcp');
  await t.prompt();
  await t.run('mcp-tui --url $DESK server 2>/dev/null | grep -E "^(Name|Version|Protocol):"');
  await t.waitFor(/Protocol:[\s\S]*❯\s*$/);
  await t.hold(2500);
  await t.run('mcp-tui --url $DESK --protocol-version 2025-11-25 server 2>/dev/null | grep -E "^(Name|Version|Protocol):"');
  await t.waitFor(/2025-11-25[\s\S]*❯\s*$/);
  await t.hold(2500);
  await t.run('mcp-tui --url http://127.0.0.1:8932/sse server 2>&1 | grep -E "Protocol|deprecated"');
  await t.waitFor(/❯\s*$/);
  await t.hold(3500);
  await t.clear();
  await t.run(`mcp-tui --url $DESK tool call schedule_callback ticket_id=T-1041 --elicit-stub '{"time":"2026-10-02T15:00:00Z"}'`);
  await t.waitFor(/Input rounds[\s\S]*❯\s*$/);
  await t.hold(6000);
}
