// A call that breaks the input schema is refused with the rule it broke;
// --skip-arg-validation sends it to see the server's own answer.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('export DESK=http://127.0.0.1:8931/mcp');
  await t.prompt();
  await t.run('mcp-tui --url $DESK tool call search_tickets limit=500');
  await t.waitFor(/maximum 50[\s\S]*❯\s*$/);
  await t.hold(4500);
  await t.run("mcp-tui --url $DESK tool call search_tickets filter:='{\"customer_id\":\"acme-7\"}'");
  await t.waitFor(/customer_id[\s\S]*❯\s*$/);
  await t.hold(4500);
  await t.clear();
  await t.run('mcp-tui --url $DESK tool call search_tickets limit=500 --skip-arg-validation');
  await t.waitFor(/Served by[\s\S]*❯\s*$/);
  await t.hold(5000);
}
