// Prompts: their arguments, completion of a value, and a rendered prompt.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('export DESK=http://127.0.0.1:8931/mcp');
  await t.prompt();
  await t.run('mcp-tui --url $DESK prompt get triage_ticket 2>/dev/null');
  await t.waitFor(/tone[\s\S]*❯\s*$/);
  await t.hold(3500);
  await t.run('mcp-tui --url $DESK prompt complete triage_ticket tone=f 2>/dev/null | jq -c .values');
  await t.waitFor(/formal[\s\S]*❯\s*$/);
  await t.hold(2500);
  await t.clear();
  await t.run('mcp-tui --url $DESK prompt execute triage_ticket ticket_id=T-1041 tone=apologetic 2>/dev/null | head -14');
  await t.waitFor(/❯\s*$/);
  await t.hold(5500);
}
