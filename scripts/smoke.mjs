/**
 * Headless ACP smoke test — verifies the handshake and one prompt turn
 * against the pi ACP adapter, without the browser in the loop.
 *
 *   node scripts/smoke.mjs [--prompt "text"] [--cwd /path]
 */
import { AcpClient } from '../server/acp-client.mjs';

const args = process.argv.slice(2);
const promptIdx = args.indexOf('--prompt');
const cwdIdx = args.indexOf('--cwd');
const prompt = promptIdx >= 0 ? args[promptIdx + 1] : 'Reply with exactly one word: pong';
const cwd = cwdIdx >= 0 ? args[cwdIdx + 1] : process.cwd();

const client = new AcpClient({ cwd });
client.on('stderr', (t) => process.stderr.write(`[agent] ${t}`));
client.on('fatal', (e) => {
  console.error('spawn failed:', e.message);
  process.exit(1);
});

client.start();

const init = await client.request('initialize', {
  protocolVersion: 1,
  clientCapabilities: { fs: { readTextFile: false, writeTextFile: false }, terminal: false },
});
console.log('initialize ok');
console.log('  protocolVersion:', init.protocolVersion);
console.log('  agentInfo:', JSON.stringify(init.agentInfo || {}));
console.log('  agentCapabilities:', JSON.stringify(init.agentCapabilities || {}));

const session = await client.request('session/new', { cwd, mcpServers: [] });
console.log('session/new ok, id =', session.sessionId);
console.log('  models:', JSON.stringify(session.models || null));

client.on('notification', (msg) => {
  if (msg.method !== 'session/update') return;
  const u = msg.params.update;
  if (u.sessionUpdate === 'agent_message_chunk') {
    process.stdout.write(u.content?.text || '');
  } else if (u.sessionUpdate === 'tool_call') {
    console.log(`\n[tool_call] ${u.kind} ${u.title} (${u.status})`);
  } else if (u.sessionUpdate !== 'agent_thought_chunk') {
    console.log(`\n[${u.sessionUpdate}]`);
  }
});

client.on('permission', ({ params, respond }) => {
  console.log(`\n[permission] ${params.toolCall?.title}`);
  const allow = (params.options || []).find((o) => String(o.kind).startsWith('allow'));
  respond({ outcome: { outcome: 'selected', optionId: allow?.optionId } });
});

console.log(`\nprompt: ${prompt}\n---`);
const result = await client.request('session/prompt', {
  sessionId: session.sessionId,
  prompt: [{ type: 'text', text: prompt }],
});
console.log(`\n---\nstopReason: ${result.stopReason}`);

await client.stop();
process.exit(0);
