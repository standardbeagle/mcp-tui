// The full matrix with a JUnit report, as a CI step would run it.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d, {setup: ['rm -f conform.xml']});
  await t.run('mcp-tui conform --report-junit conform.xml http://127.0.0.1:8931/mcp && echo "CI: green"');
  await t.waitFor(/CI: green[\s\S]*❯\s*$|FAIL[\s\S]*❯\s*$/, 90000);
  await t.hold(5000);
  await t.run('grep -c "<testcase" conform.xml');
  await t.prompt();
  await t.hold(3000);
}
