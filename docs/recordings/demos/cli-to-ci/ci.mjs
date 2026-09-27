// JSON output into jq, and a tool error turned into a failing exit code.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('export DESK=http://127.0.0.1:8931/mcp');
  await t.prompt();
  await t.run("mcp-tui --url $DESK tool call search_tickets status=open -f json 2>/dev/null | jq -r '.result.structuredContent.tickets[].id'");
  await t.waitFor(/T-1044[\s\S]*❯\s*$/);
  await t.hold(3500);
  await t.run('mcp-tui --url $DESK tool call lookup_customer customer_id=C-4040 --strict-errors; echo "exit=$?"');
  await t.waitFor(/exit=1[\s\S]*❯\s*$/);
  await t.hold(4500);
}
