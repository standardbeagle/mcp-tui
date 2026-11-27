// Text and binary resources, a URI template, and completion of its variable.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('export DESK=http://127.0.0.1:8931/mcp');
  await t.prompt();
  await t.run('mcp-tui --url $DESK resource list 2>/dev/null');
  await t.waitFor(/acme:\/\/status\/queue[\s\S]*❯\s*$/);
  await t.hold(3500);
  await t.clear();
  await t.run('mcp-tui --url $DESK resource get acme://kb/getting-started.md 2>/dev/null | head -18');
  await t.waitFor(/❯\s*$/);
  await t.hold(4000);
  await t.run('mcp-tui --url $DESK resource get acme://brand/logo.png 2>/dev/null | grep Binary');
  await t.waitFor(/bytes[\s\S]*❯\s*$/);
  await t.hold(3000);
  await t.clear();
  await t.run('mcp-tui --url $DESK resource templates 2>/dev/null');
  await t.waitFor(/tickets\/\{id\}[\s\S]*❯\s*$/);
  await t.hold(2500);
  await t.run("mcp-tui --url $DESK resource complete 'acme://tickets/{id}' id=T-104 2>/dev/null | jq -c .values");
  await t.waitFor(/T-1047[\s\S]*❯\s*$/);
  await t.hold(3500);
}
