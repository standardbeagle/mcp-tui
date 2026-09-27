// Subscribe to a resource and print each update until three have arrived.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('mcp-tui --url http://127.0.0.1:8931/mcp resource watch acme://status/queue --count 3');
  await t.waitFor(/❯\s*$/, 40000);
  await t.hold(3500);
}
