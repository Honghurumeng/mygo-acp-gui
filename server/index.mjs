import http from 'node:http';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { WebSocketServer } from 'ws';
import { AcpClient } from './acp-client.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PUBLIC_DIR = path.resolve(__dirname, '..', 'public');

const PORT = Number(process.env.PORT || 5178);
const HOST = process.env.HOST || '127.0.0.1';

const AGENT_COMMAND = process.env.ACP_AGENT_CMD || 'npx';
const AGENT_ARGS = process.env.ACP_AGENT_ARGS
  ? process.env.ACP_AGENT_ARGS.split(' ').filter(Boolean)
  : ['-y', '@geohar/pi-acp'];

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.ico': 'image/x-icon',
};

function resolveStatic(urlPath) {
  const clean = decodeURIComponent(urlPath.split('?')[0]);
  const rel = clean === '/' ? 'index.html' : clean.replace(/^\/+/, '');
  const abs = path.resolve(PUBLIC_DIR, rel);
  return abs.startsWith(PUBLIC_DIR) ? abs : null;
}

const server = http.createServer(async (req, res) => {
  try {
    const file = resolveStatic(req.url || '/');
    if (!file) {
      res.writeHead(403).end('forbidden');
      return;
    }
    const body = await readFile(file);
    res.writeHead(200, {
      'content-type': MIME[path.extname(file)] || 'application/octet-stream',
      'cache-control': 'no-store',
    });
    res.end(body);
  } catch {
    res.writeHead(404, { 'content-type': 'text/plain; charset=utf-8' }).end('not found');
  }
});

const wss = new WebSocketServer({ server, path: '/ws' });

wss.on('connection', (ws, req) => {
  const url = new URL(req.url, `http://${req.headers.host}`);
  const cwd = url.searchParams.get('cwd') || process.env.ACP_WORKSPACE || process.cwd();

  let client = null;
  let sessionId = null;
  let busy = false;
  let closed = false;
  const permissions = new Map();

  const send = (payload) => {
    if (ws.readyState === ws.OPEN) ws.send(JSON.stringify(payload));
  };
  const traffic = (dir, method, detail) => send({ type: 'traffic', dir, method, detail });

  const teardown = async () => {
    if (closed) return;
    closed = true;
    for (const p of permissions.values()) p.respond({ outcome: { outcome: 'cancelled' } });
    permissions.clear();
    if (client) await client.stop();
  };

  ws.on('close', teardown);
  ws.on('error', teardown);

  (async () => {
    send({ type: 'status', state: 'connecting', message: '正在启动 pi ACP 适配器…' });

    client = new AcpClient({ command: AGENT_COMMAND, args: AGENT_ARGS, cwd });
    client.start();

    client.on('fatal', (err) => {
      send({ type: 'status', state: 'error', message: `无法启动 agent: ${err.message}` });
    });
    client.on('exit', ({ code, signal }) => {
      if (closed) return;
      send({ type: 'status', state: 'closed', message: `agent 已退出 (code=${code}, signal=${signal})` });
    });
    client.on('stderr', (text) => send({ type: 'log', stream: 'stderr', text }));
    client.on('protocolError', ({ line }) => send({ type: 'log', stream: 'protocol', text: line }));

    client.on('notification', (msg) => {
      traffic('in', msg.method, msg.params);
      if (msg.method === 'session/update') {
        send({ type: 'update', update: msg.params.update });
      }
    });

    client.on('permission', ({ id, params, respond }) => {
      traffic('in', 'session/request_permission', params);
      permissions.set(id, { respond });
      send({ type: 'permission', id, params });
    });

    try {
      const init = await client.request('initialize', {
        protocolVersion: 1,
        clientCapabilities: {
          fs: { readTextFile: false, writeTextFile: false },
          terminal: false,
        },
      });
      traffic('out', 'initialize', null);

      const session = await client.request('session/new', { cwd, mcpServers: [] });
      traffic('out', 'session/new', { cwd });
      sessionId = session.sessionId;

      send({
        type: 'hello',
        cwd,
        sessionId,
        agent: init.agentInfo || null,
        capabilities: init.agentCapabilities || {},
        models: session.models || null,
        configOptions: session.configOptions || null,
        modes: session.modes || null,
        agentCommand: `${AGENT_COMMAND} ${AGENT_ARGS.join(' ')}`,
      });
      send({ type: 'status', state: 'ready', message: '已连接' });
    } catch (error) {
      const methods = error.data?.authMethods || [];
      if (error.code === -32000 || /authenticat/i.test(error.message || '')) {
        send({ type: 'authRequired', message: error.message, methods });
        send({ type: 'status', state: 'error', message: 'pi 未配置凭据' });
      } else {
        send({ type: 'status', state: 'error', message: `握手失败: ${error.message}` });
      }
    }
  })();

  ws.on('message', async (raw) => {
    let msg;
    try {
      msg = JSON.parse(raw.toString());
    } catch {
      return;
    }
    if (!client) return;

    try {
      switch (msg.type) {
        case 'prompt': {
          if (!sessionId) {
            send({ type: 'error', message: '会话尚未就绪' });
            return;
          }
          if (busy) {
            send({ type: 'error', message: '上一轮还在进行中' });
            return;
          }
          busy = true;
          send({ type: 'turnStart' });
          traffic('out', 'session/prompt', { text: msg.text });
          try {
            const result = await client.request('session/prompt', {
              sessionId,
              prompt: [{ type: 'text', text: msg.text }],
            });
            send({ type: 'turnEnd', stopReason: result?.stopReason || 'end_turn' });
          } catch (error) {
            send({ type: 'error', message: `prompt 失败: ${error.message}` });
            send({ type: 'turnEnd', stopReason: 'error' });
          } finally {
            busy = false;
          }
          break;
        }
        case 'cancel': {
          if (!sessionId) return;
          client.notify('session/cancel', { sessionId });
          traffic('out', 'session/cancel', null);
          break;
        }
        case 'permission': {
          const pending = permissions.get(msg.id);
          if (!pending) return;
          permissions.delete(msg.id);
          if (!msg.optionId) {
            pending.respond({ outcome: { outcome: 'cancelled' } });
          } else {
            pending.respond({ outcome: { outcome: 'selected', optionId: msg.optionId } });
          }
          traffic('out', 'permission/response', { optionId: msg.optionId || null });
          break;
        }
        case 'setConfig':
        case 'setModel': {
          if (!sessionId) return;
          const configId = msg.configId || 'model';
          const value = msg.value ?? msg.modelId;
          if (!value) return;
          try {
            const res = await client.request('session/set_config_option', { sessionId, configId, value });
            traffic('out', 'session/set_config_option', { configId, value });
            if (res?.configOptions) send({ type: 'configOptions', configOptions: res.configOptions });
          } catch (error) {
            if (error.code !== -32601) throw error;
            // 适配器没实现 config option 时的兜底：model 走 unstable set_model，模式走 set_mode
            if (configId === 'model') {
              await client.request('session/set_model', { sessionId, modelId: value });
              traffic('out', 'session/set_model', { modelId: value });
            } else if (configId === 'thought_level' || configId === 'mode') {
              await client.request('session/set_mode', { sessionId, modeId: value });
              traffic('out', 'session/set_mode', { modeId: value });
            } else {
              throw error;
            }
          }
          break;
        }
        case 'newSession': {
          const session = await client.request('session/new', { cwd, mcpServers: [] });
          sessionId = session.sessionId;
          send({
            type: 'session',
            sessionId,
            models: session.models || null,
            modes: session.modes || null,
            configOptions: session.configOptions || null,
          });
          break;
        }
        default:
          break;
      }
    } catch (error) {
      send({ type: 'error', message: error.message });
    }
  });
});

server.listen(PORT, HOST, () => {
  console.log(`[acp-pi-gui] http://${HOST}:${PORT}`);
  console.log(`[acp-pi-gui] agent: ${AGENT_COMMAND} ${AGENT_ARGS.join(' ')}`);
});

for (const sig of ['SIGINT', 'SIGTERM']) {
  process.on(sig, () => {
    server.close();
    process.exit(0);
  });
}
