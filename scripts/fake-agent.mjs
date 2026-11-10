/**
 * 极简假 ACP agent，用于在没有 pi 的环境里调试 ACP 客户端与 UI。
 *   node scripts/fake-agent.mjs
 */
import readline from 'node:readline';

const MODELS = ['demo/small', 'demo/large', 'demo/reasoning'];
const THINKING = ['off', 'minimal', 'low', 'medium', 'high', 'xhigh'];

let currentModel = 'demo/small';
let currentThinking = 'medium';

const state = {
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

const send = (payload) => process.stdout.write(`${JSON.stringify(payload)}\n`);

process.stderr.write('fake-agent ready\n');

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

  switch (method) {
    case 'initialize':
      send({
        jsonrpc: '2.0',
        id,
        result: {
          protocolVersion: 1,
          agentInfo: { name: 'fake-agent', title: 'Fake ACP agent', version: '0.0.1' },
          agentCapabilities: {},
        },
      });
      return;

    case 'session/new':
      send({
        jsonrpc: '2.0',
        id,
        result: { sessionId: 'fake-session', configOptions: state.configOptions(), models: state.models, modes: [] },
      });
      return;

    case 'session/set_config_option':
      if (params.configId === 'model' && MODELS.includes(params.value)) currentModel = params.value;
      else if (params.configId === 'thought_level' && THINKING.includes(params.value)) currentThinking = params.value;
      send({ jsonrpc: '2.0', id, result: { configOptions: state.configOptions() } });
      return;

    case 'session/prompt': {
      const text = params.prompt?.[0]?.text ?? '';
      send({
        jsonrpc: '2.0',
        method: 'session/update',
        params: {
          sessionId: params.sessionId,
          update: {
            sessionUpdate: 'agent_message_chunk',
            content: {
              type: 'text',
              text: `收到：${text}\n\n当前模型 \`${currentModel}\`，思考强度 \`${currentThinking}\`。`,
            },
          },
        },
      });
      setTimeout(() => send({ jsonrpc: '2.0', id, result: { stopReason: 'end_turn' } }), 150);
      return;
    }

    default:
      send({ jsonrpc: '2.0', id, error: { code: -32601, message: `Method not found: ${method}` } });
  }
});
