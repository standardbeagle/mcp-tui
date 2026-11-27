// A stdio server that prints a banner to stdout, then one that ignores
// server/discover: the first is quoted, the second named with its fix.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('mcp-tui --cmd demo-server --args -stdio,-stdout-banner tool list');
  await t.waitFor(/Suggestion[\s\S]*❯\s*$/);
  await t.hold(5000);
  await t.clear();
  await t.run('mcp-tui --timeout 3s --cmd demo-server --args -stdio,-ignore-discover tool list');
  await t.waitFor(/Suggestion[\s\S]*❯\s*$/);
  await t.hold(5500);
  await t.run('mcp-tui --protocol-version 2025-11-25 --cmd demo-server --args -stdio,-ignore-discover server');
  await t.waitFor(/2025-11-25[\s\S]*❯\s*$/);
  await t.hold(3000);
}
