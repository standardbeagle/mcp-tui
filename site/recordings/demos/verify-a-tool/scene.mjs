// Connect over stdio, run search_tickets from the schema-built form, then
// run the CLI command the TUI shows for the same call.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('mcp-tui --cmd demo-server --args -stdio');
  await t.waitFor(/Search tickets/);
  await t.hold(1500);

  // Walk down to search_tickets so the detail pane shows each tool on the way.
  for (let i = 0; i < 6; i++) {
    await t.press('ArrowDown');
    await t.hold(350);
  }
  await t.hold(1200);
  await t.press('Enter');
  await t.waitFor(/Execute Tool: Search tickets/);
  await t.hold(1500);

  await t.press('Tab');                  // limit
  await t.type('3');
  await t.hold(400);
  await t.press('Tab', 2);               // status
  await t.type('open');
  await t.hold(700);
  await t.press('Enter');
  await t.waitFor(/T-10\d\d/);
  await t.hold(3500);

  await t.press('Tab', 3);               // tags, Execute, CLI
  await t.press('Enter');
  await t.waitFor(/Equivalent CLI command/);
  await t.hold(3500);

  await t.quitTUI();
  await t.run('mcp-tui --cmd demo-server --args -stdio tool call search_tickets limit=3 status=open');
  await t.waitFor(/T-10\d\d[\s\S]*❯\s*$/);
  await t.hold(3500);
}
