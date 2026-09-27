// A streamable HTTP server reached at the wrong path, then with the wrong
// transport flag: each failure names the HTTP status and the fix.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('mcp-tui --url http://127.0.0.1:8931/api tool list');
  await t.waitFor(/Suggested actions[\s\S]*❯\s*$/);
  await t.hold(4500);
  await t.clear();
  await t.run('mcp-tui --transport sse --url http://127.0.0.1:8931/mcp tool list');
  await t.waitFor(/Suggested actions[\s\S]*❯\s*$/);
  await t.hold(4500);
  await t.clear();
  await t.run('mcp-tui --url http://127.0.0.1:8931/mcp tool list');
  await t.waitFor(/Total: 7 tools[\s\S]*❯\s*$/);
  await t.hold(2500);
}
