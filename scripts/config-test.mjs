/**
 * 验证模型/思考强度切换走 session/set_config_option。
 *   node scripts/config-test.mjs
 */
import WebSocket from 'ws';

const ws = new WebSocket('ws://127.0.0.1:5178/ws');
let step = 0;
let target = null;

const timer = setTimeout(() => {
  console.log('\nTIMEOUT');
  process.exit(1);
}, 60000);

const log = (...a) => console.log(...a);

ws.on('message', (raw) => {
  const msg = JSON.parse(raw.toString());

  if (msg.type === 'traffic') {
    if (/set_|error/i.test(msg.method)) log(`  [acp ${msg.dir}] ${msg.method}`);
    return;
  }

  if (msg.type === 'hello') {
    const models = msg.configOptions || [];
    log('configOptions:', models.map((c) => c.id).join(', '));
    const modelOpt = models.find((c) => c.id === 'model');
    const thinkOpt = models.find((c) => c.id === 'thought_level');
    target = modelOpt?.options.find((o) => o.value !== modelOpt.currentValue)?.value;
    log('current model:', modelOpt?.currentValue, '| thinking:', thinkOpt?.currentValue);
    log('switching model ->', target);
    ws.send(JSON.stringify({ type: 'setConfig', configId: 'model', value: target }));
    step = 1;
    return;
  }

  if (msg.type === 'configOptions' && step === 1) {
    const modelOpt = msg.configOptions.find((c) => c.id === 'model');
    log('after switch, model =', modelOpt?.currentValue);
    if (modelOpt?.currentValue !== target) {
      log('FAIL: model did not change');
      process.exit(1);
    }
    log('switching thinking -> high');
    ws.send(JSON.stringify({ type: 'setConfig', configId: 'thought_level', value: 'high' }));
    step = 2;
    return;
  }

  if (msg.type === 'configOptions' && step === 2) {
    const thinkOpt = msg.configOptions.find((c) => c.id === 'thought_level');
    log('after switch, thinking =', thinkOpt?.currentValue);
    const ok = thinkOpt?.currentValue === 'high';
    log(ok ? 'PASS' : 'FAIL');
    clearTimeout(timer);
    ws.close();
    process.exit(ok ? 0 : 1);
  }

  if (msg.type === 'error') {
    log('ERROR:', msg.message);
  }
});
