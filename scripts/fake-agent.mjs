/**
 * 极简假 ACP agent，用于在没有 pi 的环境里调试 ACP 客户端与 UI。
 *   node scripts/fake-agent.mjs
 *
 * 支持的测试指令（写在 prompt 文本里）：
 *   /slow         每块间隔更久，便于测试 Stop / session/cancel
 *   /permission   先发一个 tool_call，再请求权限
 *   /thought      先流一段 agent_thought_chunk
 *   /mcp          回复最近一次 session/new 收到的 MCP server 数
 *   /image        回复收到的图片内容块数量
 *   /file         回复收到的 resource_link / resource 内容块数量
 *   /turnusage    不发 usage_update，只在 prompt 结果里返回 usage
 *   /tinyusage    发一个小于 1% 的 usage_update
 *   /notice       先发 startup / auto_retry 状态块，再正常回复
 *
 * 环境变量：
 *   FAKE_AGENT_CHILD_PID_FILE  启动一个子进程并把 pid 写入该文件，
 *                              用于验证关闭时会杀掉整个进程组。
 *   FAKE_AGENT_NO_SESSION      让 session/new 返回 -32000，模拟需要登录的 agent。
 */
import { spawn } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import readline from 'node:readline';

const MODELS = ['demo/small', 'demo/large', 'demo/reasoning'];
const THINKING = ['off', 'minimal', 'low', 'medium', 'high', 'xhigh'];
const MODES = [
  { id: 'default', name: 'Standard', description: 'Ask before tools.' },
  { id: 'plan', name: 'Plan', description: 'Plan only.' },
];

const HISTORY_IMAGE = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=';

let currentModel = 'demo/small';
let currentThinking = 'medium';
let currentMode = 'default';
let sessionCounter = 0;
let lastSessionId = 'fake-session';
let lastMCPServers = [];

const state = {
  get modes() {
    return { currentModeId: currentMode, availableModes: MODES };
  },
  get models() {
    return {
      availableModels: MODELS.map((id) => ({ modelId: id, name: id, description: null })),
      currentModelId: currentModel,
    };
  },
  configOptions: () => [
    {
      type: 'select',
      id: 'model',
      category: 'model',
      name: 'Model',
      currentValue: currentModel,
      options: MODELS.map((id) => ({ value: id, name: `demo/${id.split('/')[1]}`, description: null })),
    },
    {
      type: 'select',
      id: 'thought_level',
      category: 'thought_level',
      name: 'Thinking',
      currentValue: currentThinking,
      options: THINKING.map((v) => ({ value: v, name: v, description: null })),
    },
  ],
};

const send = (payload) => {
  try {
    process.stdout.write(`${JSON.stringify(payload)}\n`);
  } catch {
    // The client closed the pipe; nothing left to tell.
  }
};

if (process.env.FAKE_AGENT_CHILD_PID_FILE) {
  const child = spawn(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], {
    stdio: 'ignore',
  });
  child.unref();
  writeFileSync(process.env.FAKE_AGENT_CHILD_PID_FILE, String(child.pid));
}

process.stdout.on('error', () => process.exit(0));

process.stderr.write('fake-agent ready\n');

let active = null;
let nextPermissionId = 100_000;
const pendingPermissions = new Map();

function sendUpdate(params) {
  send({ jsonrpc: '2.0', method: 'session/update', params });
}

function sessionState(extra = {}) {
  return {
    configOptions: state.configOptions(),
    models: state.models,
    modes: state.modes,
    ...extra,
  };
}

function publishCommands(sessionId) {
  sendUpdate({
    sessionId,
    update: {
      sessionUpdate: 'available_commands_update',
      availableCommands: [
        { name: 'compact', description: 'Summarize the conversation', input: null },
        { name: 'plan', description: 'Plan before acting', input: { hint: 'arguments' } },
      ],
    },
  });
}

function finishTurn(turn, reason = 'end_turn', usage = null) {
  if (!turn || turn.finished) return;
  turn.finished = true;
  if (turn.timer) clearTimeout(turn.timer);
  if (active === turn) active = null;
  const result = { stopReason: reason };
  if (usage) result.usage = usage;
  send({ jsonrpc: '2.0', id: turn.id, result });
}

function sendChunk(turn, text, meta = null) {
  const update = {
    sessionUpdate: 'agent_message_chunk',
    content: { type: 'text', text },
  };
  if (meta) update._meta = meta;
  sendUpdate({ sessionId: turn.sessionId, update });
}

function requestPermission(turn) {
  const permissionId = nextPermissionId++;
  const toolCallId = `tool-${permissionId}`;
  sendUpdate({
    sessionId: turn.sessionId,
    update: {
      sessionUpdate: 'tool_call',
      toolCallId,
      title: 'Fake tool needs approval',
      kind: 'execute',
      status: 'pending',
      rawInput: { command: 'echo fake' },
    },
  });
  pendingPermissions.set(permissionId, (result) => {
    const outcome = result?.outcome ?? {};
    const selected = outcome.outcome === 'selected' ? outcome.optionId : 'cancelled';
    sendUpdate({
      sessionId: turn.sessionId,
      update: {
        sessionUpdate: 'tool_call_update',
        toolCallId,
        status: selected.startsWith('allow') ? 'completed' : 'failed',
      },
    });
    if (selected.startsWith('allow')) {
      sendChunk(turn, 'permission granted');
      finishTurn(turn);
    } else if (outcome.outcome === 'cancelled') {
      finishTurn(turn, 'cancelled');
    } else {
      sendChunk(turn, 'permission denied');
      finishTurn(turn);
    }
  });
  send({
    jsonrpc: '2.0',
    id: permissionId,
    method: 'session/request_permission',
    params: {
      sessionId: turn.sessionId,
      toolCall: {
        toolCallId,
        title: 'Fake tool needs approval',
        kind: 'execute',
        status: 'pending',
        rawInput: { command: 'echo fake', description: 'Echo a fake tool command' },
      },
      options: [
        { optionId: 'allow-once', name: 'Allow once', kind: 'allow_once' },
        { optionId: 'allow-always', name: 'Allow always', kind: 'allow_always' },
        { optionId: 'reject-once', name: 'Reject', kind: 'reject_once' },
      ],
    },
  });
}

function startTurn(turn, text, images = 0, mcpCount = 0, files = 0, embedded = 0) {
  const slow = text.includes('/slow');
  const permission = text.includes('/permission');
  const thought = text.includes('/thought');
  const plan = text.includes('/plan');
  const turnUsage = text.includes('/turnusage');

  sendUpdate({
    sessionId: turn.sessionId,
    update: { sessionUpdate: 'session_info_update', title: 'Fake session title' },
  });
  if (!turnUsage) {
    const tiny = text.includes('/tinyusage');
    sendUpdate({
      sessionId: turn.sessionId,
      update: {
        sessionUpdate: 'usage_update',
        used: tiny ? 1600 : 1200,
        size: tiny ? 1_000_000 : 10000,
        cost: { amount: 0.0012, currency: 'USD' },
      },
    });
  }
  if (plan) {
    sendUpdate({
      sessionId: turn.sessionId,
      update: {
        sessionUpdate: 'plan',
        entries: [
          { content: 'Inspect the request', status: 'completed', priority: 'high' },
          { content: 'Write the answer', status: 'in_progress', priority: 'medium' },
        ],
      },
    });
  }

  if (thought) {
    sendUpdate({
      sessionId: turn.sessionId,
      update: {
        sessionUpdate: 'agent_thought_chunk',
        content: { type: 'text', text: 'Let me think about that…' },
      },
    });
  }

  if (text.includes('/notice')) {
    sendChunk(turn, 'pi v9.9.9\n\n## Extensions\n- /tmp/fake-extension.ts\n', {
      piAcp: { startupInfo: true },
    });
    sendChunk(turn, 'Retrying (attempt 1/3, waiting 2s)...', {
      piAcp: { notice: { kind: 'auto_retry', level: 'warning' } },
    });
    sendChunk(turn, 'Retry finished, resuming.', {
      piAcp: { notice: { kind: 'auto_retry', level: 'info' } },
    });
  }

  if (permission) {
    requestPermission(turn);
    return;
  }

  const reply = `收到：${text}\n\n当前模型 \`${currentModel}\`，思考强度 \`${currentThinking}\`，模式 \`${currentMode}\`。\nimages=${images} files=${files} embedded=${embedded} mcp=${mcpCount}`;
  const chars = Array.from(reply);
  const chunkSize = slow ? 2 : Math.max(1, Math.ceil(chars.length / 8));
  let offset = 0;

  const tick = () => {
    if (turn.finished) return;
    if (turn.cancelled) {
      finishTurn(turn, 'cancelled');
      return;
    }
    if (offset >= chars.length) {
      finishTurn(turn, 'end_turn', turnUsage ? {
        totalTokens: 1300,
        inputTokens: 1200,
        outputTokens: 100,
        thoughtTokens: 40,
        cachedReadTokens: 900,
      } : null);
      return;
    }
    sendChunk(turn, chars.slice(offset, offset + chunkSize).join(''));
    offset += chunkSize;
    turn.timer = setTimeout(tick, slow ? 80 : 35);
  };
  tick();
}

const rl = readline.createInterface({ input: process.stdin });
rl.on('line', (line) => {
  if (!line.trim()) return;
  let req;
  try {
    req = JSON.parse(line);
  } catch {
    return;
  }
  const { id, method, params } = req;

  // Response to an agent -> client request such as request_permission.
  if (!method && id !== undefined && ('result' in req || 'error' in req)) {
    const handler = pendingPermissions.get(id);
    if (handler) {
      pendingPermissions.delete(id);
      handler(req.result);
    }
    return;
  }

  switch (method) {
    case 'initialize':
      send({
        jsonrpc: '2.0',
        id,
        result: {
          protocolVersion: 1,
          agentInfo: { name: 'fake-agent', title: 'Fake ACP agent', version: '0.0.1' },
          agentCapabilities: {
            loadSession: true,
            promptCapabilities: { audio: false, image: true, embeddedContext: true },
            mcpCapabilities: { http: true, sse: false },
            sessionCapabilities: { list: {}, close: {} },
          },
        },
      });
      return;

    case 'session/new': {
      if (process.env.FAKE_AGENT_NO_SESSION) {
        send({
          jsonrpc: '2.0',
          id,
          error: { code: -32000, message: 'authentication required' },
        });
        return;
      }
      sessionCounter += 1;
      lastSessionId = sessionCounter === 1 ? 'fake-session' : `fake-session-${sessionCounter}`;
      lastMCPServers = params.mcpServers ?? [];
      send({
        jsonrpc: '2.0',
        id,
        result: sessionState({ sessionId: lastSessionId }),
      });
      publishCommands(lastSessionId);
      return;
    }

    case 'session/list':
      send({
        jsonrpc: '2.0',
        id,
        result: {
          sessions: [
            {
              sessionId: 'fake-history',
              cwd: process.cwd(),
              title: 'Fake history',
              updatedAt: '2026-01-01T00:00:00Z',
            },
          ],
        },
      });
      return;

    case 'session/load': {
      lastSessionId = params.sessionId;
      sendUpdate({
        sessionId: lastSessionId,
        update: { sessionUpdate: 'user_message_chunk', content: { type: 'text', text: 'Question from history' } },
      });
      sendUpdate({
        sessionId: lastSessionId,
        update: {
          sessionUpdate: 'user_message_chunk',
          content: { type: 'image', mimeType: 'image/png', data: HISTORY_IMAGE },
        },
      });
      sendUpdate({
        sessionId: lastSessionId,
        update: {
          sessionUpdate: 'agent_thought_chunk',
          content: { type: 'text', text: 'Reasoning from history' },
        },
      });
      sendUpdate({
        sessionId: lastSessionId,
        update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: 'Answer from history' } },
      });
      send({ jsonrpc: '2.0', id, result: sessionState() });
      publishCommands(lastSessionId);
      return;
    }

    case 'session/close':
      if (active) {
        finishTurn(active, 'cancelled');
      }
      send({ jsonrpc: '2.0', id, result: {} });
      return;

    case 'session/set_mode':
      if (!MODES.some((mode) => mode.id === params.modeId)) {
        send({ jsonrpc: '2.0', id, error: { code: -32602, message: `Unknown mode: ${params.modeId}` } });
        return;
      }
      currentMode = params.modeId;
      sendUpdate({
        sessionId: params.sessionId,
        update: { sessionUpdate: 'current_mode_update', currentModeId: currentMode },
      });
      send({ jsonrpc: '2.0', id, result: {} });
      return;

    case 'session/set_config_option':
      if (params.configId === 'model' && MODELS.includes(params.value)) currentModel = params.value;
      else if (params.configId === 'thought_level' && THINKING.includes(params.value)) currentThinking = params.value;
      else {
        send({ jsonrpc: '2.0', id, error: { code: -32602, message: `Unknown config: ${params.configId}=${params.value}` } });
        return;
      }
      sendUpdate({
        sessionId: params.sessionId,
        update: { sessionUpdate: 'config_option_update', configOptions: state.configOptions() },
      });
      send({ jsonrpc: '2.0', id, result: { configOptions: state.configOptions() } });
      return;

    case 'session/prompt': {
      if (active) {
        finishTurn(active, 'cancelled');
      }
      const blocks = params.prompt ?? [];
      const text = blocks.filter((b) => b.type === 'text').map((b) => b.text).join('\n');
      const images = blocks.filter((b) => b.type === 'image').length;
      const files = blocks.filter((b) => b.type === 'resource_link' || b.type === 'resource').length;
      const embedded = blocks.filter((b) => b.type === 'resource').length;
      const turn = {
        id,
        sessionId: params.sessionId,
        cancelled: false,
        finished: false,
        timer: null,
      };
      active = turn;
      startTurn(turn, text, images, lastMCPServers.length, files, embedded);
      return;
    }

    case 'session/cancel': {
      if (!active) return;
      active.cancelled = true;
      if (active.timer) clearTimeout(active.timer);
      // A turn waiting for a permission answer is settled now. The client
      // may still answer the permission request; it is ignored.
      if (active.finished) return;
      finishTurn(active, 'cancelled');
      return;
    }

    default:
      send({ jsonrpc: '2.0', id, error: { code: -32601, message: `Method not found: ${method}` } });
  }
});

rl.on('close', () => {
  if (active) finishTurn(active, 'cancelled');
  process.exit(0);
});
