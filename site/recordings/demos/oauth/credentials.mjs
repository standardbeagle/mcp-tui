// A server behind OAuth: the 401 is named with the flags that sign in, then
// client credentials get a token (the flow's steps shown at info level).
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d, {setup: ['rm -rf .cache']});
  await t.run('mcp-tui --url http://127.0.0.1:8933/mcp tool list');
  await t.waitFor(/Suggested actions[\s\S]*❯\s*$/);
  await t.hold(5000);
  await t.clear();
  await t.run('mcp-tui --url http://127.0.0.1:8933/mcp --oauth-client-id acme-demo --oauth-client-secret demo-secret \\');
  await t.run('    --log-level info tool list 2>&1 | grep -oE "\\[oauth\\] [A-Z][a-z ]+|Available Tools.*"');
  await t.waitFor(/Available Tools[\s\S]*❯\s*$/, 40000);
  await t.hold(6000);
}
