/**
 * 直接对适配器验证 config option 切换（不经 Web 层）。
 *   node scripts/config-smoke.mjs
 */
import { AcpClient } from '../server/acp-client.mjs';

const client = new AcpClient({ cwd: process.cwd() });
client.on('stderr', (t) => process.stderr.write(`[agent] ${t}`));
client.start();

const init = await client.request('initialize', {
  protocolVersion: 1,
  clientCapabilities: { fs: { readTextFile: false, writeTextFile: false }, terminal: false },
});
console.log('initialize ok, protocolVersion =', init.protocolVersion);

const session = await client.request('session/new', { cwd: process.cwd(), mcpServers: [] });
const options = session.configOptions || [];
console.log('configOptions:', options.map((o) => `${o.id}(${o.currentValue})`).join(', '));

const model = options.find((o) => o.id === 'model');
const think = options.find((o) => o.id === 'thought_level');

if (!model) {
  console.log('SKIP: no model config option');
  await client.stop();
  process.exit(1);
}

const target = model.options.find((o) => o.value !== model.currentValue)?.value;
console.log(`set model: ${model.currentValue} -> ${target}`);
const r1 = await client.request('session/set_config_option', {
  sessionId: session.sessionId,
  configId: 'model',
  value: target,
});
const after1 = (r1.configOptions || []).find((o) => o.id === 'model');
console.log('  now:', after1?.currentValue, after1?.currentValue === target ? 'OK' : 'MISMATCH');

console.log(`set thinking: ${think?.currentValue} -> high`);
const r2 = await client.request('session/set_config_option', {
  sessionId: session.sessionId,
  configId: 'thought_level',
  value: 'high',
});
const after2 = (r2.configOptions || []).find((o) => o.id === 'thought_level');
console.log('  now:', after2?.currentValue, after2?.currentValue === 'high' ? 'OK' : 'MISMATCH');

const pass = after1?.currentValue === target && after2?.currentValue === 'high';
console.log(pass ? '\nPASS' : '\nFAIL');
await client.stop();
process.exit(pass ? 0 : 1);
