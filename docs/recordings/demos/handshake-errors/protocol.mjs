// A server that answers a request nobody sent and spells a notification
// wrong: the SDK drops both silently; mcp-tui names them, and the verify
// probe fails on them.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('mcp-tui --cmd demo-server --args -stdio,-stray-messages tool list 2>&1 | tail -4');
  await t.waitFor(/⚠ protocol[\s\S]*❯\s*$/);
  await t.hold(5000);
  await t.run('mcp-tui verify --probe protocol-violations --cmd demo-server --args -stdio,-stray-messages');
  await t.waitFor(/failed[\s\S]*❯\s*$/);
  await t.hold(5500);
}
