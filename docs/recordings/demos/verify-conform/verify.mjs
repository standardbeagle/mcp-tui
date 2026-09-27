// verify against a server that breaks three rules, then a clean one.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('mcp-tui verify http://127.0.0.1:8934/mcp');
  await t.waitFor(/skipped[\s\S]*❯\s*$/, 60000);
  await t.hold(6500);
  await t.clear();
  await t.run('mcp-tui verify http://127.0.0.1:8931/mcp');
  await t.waitFor(/skipped[\s\S]*❯\s*$/, 60000);
  await t.hold(3500);
}
