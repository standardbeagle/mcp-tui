// A tool call run as an MCP task: task status notifications stream while
// --wait follows the task to its result.
import {terminal} from '../../term.mjs';

export default async function run(d) {
  const t = await terminal(d);
  await t.run('mcp-tui --cmd mcp-server-everything --args stdio --watch-notifications \\');
  await t.run('    tool call simulate-research-query topic="MCP transports" --task --wait | head -6');
  await t.waitFor(/Research Report[\s\S]*❯\s*$/, 60000);
  await t.hold(5000);
}
