// schedule_callback needs a time: the server's form opens mid-call. Then
// draft_reply asks for a sampling reply.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('mcp-tui --url http://127.0.0.1:8931/mcp');
  await t.waitFor(/Schedule callback/);
  await t.press('ArrowDown', 5);
  await t.press('Enter');
  await t.waitFor(/Execute Tool: Schedule callback/);
  await t.type('T-1041');
  await t.hold(600);
  await t.press('Enter');
  await t.waitFor(/Elicitation Request/, 20000);
  await t.hold(3000);
  await t.press('Tab');                    // past the optional phone number
  await t.type('2026-10-02T15:00:00Z');
  await t.hold(1000);
  await t.press('Control+s');
  await t.waitFor(/Callback booked/, 20000);
  await t.hold(4000);
  await t.press('Escape');
  await t.hold(500);
  await t.press('ArrowUp', 3);             // draft_reply
  await t.press('Enter');
  await t.waitFor(/Execute Tool: Draft reply/);
  await t.type('T-1042');
  await t.press('Enter');
  await t.waitFor(/Sampling Request/, 20000);
  await t.hold(3500);
  await t.press('1');                      // manual reply
  await t.type('Thanks for the report, Priya. The CSV export fix ships in today\'s release. The Acme support team');
  await t.hold(1000);
  await t.press('Control+s');
  await t.waitFor(/Served by/, 20000);
  await t.hold(4000);
  await t.quitTUI();
}
