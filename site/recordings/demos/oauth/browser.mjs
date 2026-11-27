// Dynamic client registration and the authorization-code flow: mcp-tui
// says where to sign in, waits for the redirect, and caches the token.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d, {setup: ['rm -rf .cache']});
  await t.run('mcp-tui --url http://127.0.0.1:8933/mcp --oauth-dynamic-registration tool list | head -4');
  await t.waitFor(/Available Tools[\s\S]*❯\s*$/, 40000);
  await t.hold(5000);
  await t.run('mcp-tui --url http://127.0.0.1:8933/mcp --oauth-dynamic-registration server 2>/dev/null | head -5');
  await t.waitFor(/Name:[\s\S]*❯\s*$/, 40000);
  await t.hold(4000);
}
