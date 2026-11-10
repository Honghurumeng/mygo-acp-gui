/**
 * Browser-less check of the web layer: connects over WebSocket exactly like
 * public/app.js does, then runs one prompt turn.
 *
 *   node scripts/ws-test.mjs [ws://127.0.0.1:5178/ws]
 */
import WebSocket from 'ws';

const url = process.argv[2] || 'ws://127.0.0.1:5178/ws';
const ws = new WebSocket(url);
let sent = false;

const timer = setTimeout(() => {
  console.log('\nTIMEOUT');
  process.exit(1);
}, 90000);

ws.on('open', () => console.log('ws open', url));

ws.on('message', (raw) => {
  const msg = JSON.parse(raw.toString());

  if (msg.type === 'traffic') {
    console.log(`[acp ${msg.dir}] ${msg.method}`);
    return;
  }
  if (msg.type === 'update') {
    const u = msg.update;
    if (u.sessionUpdate === 'agent_message_chunk') process.stdout.write(u.content?.text || '');
    else if (u.sessionUpdate === 'tool_call') console.log(`\n[tool_call] ${u.kind} ${u.title}`);
    return;
  }
  if (msg.type === 'permission') {
    const allow = (msg.params.options || []).find((o) => String(o.kind).startsWith('allow'));
    ws.send(JSON.stringify({ type: 'permission', id: msg.id, optionId: allow?.optionId }));
    console.log(`\n[permission] ${msg.params.toolCall?.title} -> allow`);
    return;
  }

  console.log('[msg]', msg.type, msg.message || msg.state || msg.text || '');
  if (msg.type === 'hello') {
    console.log('  session:', msg.sessionId);
    console.log('  model:', msg.models?.currentModelId);
    if (!sent) {
      sent = true;
      ws.send(JSON.stringify({ type: 'prompt', text: 'reply with exactly: ws-ok' }));
    }
  }
  if (msg.type === 'turnEnd') {
    console.log(`\n[turnEnd] ${msg.stopReason}`);
    clearTimeout(timer);
    ws.close();
    process.exit(0);
  }
  if (msg.type === 'authRequired') {
    clearTimeout(timer);
    ws.close();
    process.exit(2);
  }
});
