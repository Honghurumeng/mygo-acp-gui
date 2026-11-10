const els = {
  messages: document.getElementById('messages'),
  empty: document.getElementById('empty'),
  input: document.getElementById('input'),
  send: document.getElementById('send'),
  stop: document.getElementById('stop'),
  status: document.getElementById('status'),
  statusText: document.getElementById('statusText'),
  cwd: document.getElementById('cwd'),
  configs: document.getElementById('configs'),
  permissionHost: document.getElementById('permissionHost'),
  traffic: document.getElementById('traffic'),
  trafficBody: document.getElementById('trafficBody'),
};

let ws = null;
let ready = false;
let busy = false;
let stream = null;
let thought = null;
let planEl = null;
let hasPrompted = false;

const START_CWD = new URLSearchParams(location.search).get('cwd') || '';

function connect() {
  const qs = START_CWD ? `?cwd=${encodeURIComponent(START_CWD)}` : '';
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  ws = new WebSocket(`${proto}://${location.host}/ws${qs}`);

  ws.addEventListener('open', () => setStatus('connecting', '握手中'));
  ws.addEventListener('close', () => {
    ready = false;
    setStatus('closed', '已断开');
    setTimeout(connect, 1500);
  });
  ws.addEventListener('error', () => setStatus('error', '连接出错'));
  ws.addEventListener('message', (event) => {
    let msg;
    try {
      msg = JSON.parse(event.data);
    } catch {
      return;
    }
    handle(msg);
  });
}

function setStatus(state, text) {
  els.status.className = `status is-${state}`;
  els.statusText.textContent = text;
}

function handle(msg) {
  switch (msg.type) {
    case 'status':
      setStatus(msg.state, msg.message);
      if (msg.state === 'error' || msg.state === 'closed') {
        ready = false;
        addSystem(msg.message, msg.state === 'error');
      }
      break;

    case 'hello':
      ready = true;
      if (msg.cwd) {
        els.cwd.textContent = msg.cwd;
        els.cwd.title = msg.cwd;
      }
      setupConfigs(msg.configOptions, msg.models);
      break;

    case 'session':
      setupConfigs(msg.configOptions, msg.models);
      clearConversation();
      addSystem(`新会话 ${String(msg.sessionId).slice(0, 8)}`);
      break;

    case 'update':
      onUpdate(msg.update);
      break;

    case 'permission':
      showPermission(msg);
      break;

    case 'authRequired':
      showAuthCard(msg);
      break;

    case 'turnStart':
      setBusy(true);
      break;

    case 'turnEnd':
      setBusy(false);
      closeBubble();
      if (msg.stopReason && msg.stopReason !== 'end_turn') {
        addSystem(`本轮结束：${msg.stopReason}`);
      }
      break;

    case 'error':
      addSystem(msg.message, true);
      break;

    case 'traffic':
      addTraffic(msg);
      break;

    case 'configOptions':
      setupConfigs(msg.configOptions, msg.models);
      break;

    default:
      if (msg.type === 'log' && msg.stream === 'stderr') {
        // 适配器对已由我们回退处理的方法会打 "Error handling request"，
        // 这类噪音不该当成致命错误弹给用户。
        if (/Error handling request/i.test(msg.text)) return;
        if (/error|invalid|fail/i.test(msg.text)) addSystem(msg.text.trim(), true);
      }
  }
}

const THINK_LABELS = {
  off: '思考 关',
  minimal: '思考 极简',
  low: '思考 低',
  medium: '思考 中',
  high: '思考 高',
  xhigh: '思考 极高',
};

function optionLabel(configId, option, fallback) {
  if (configId === 'thought_level') return THINK_LABELS[option.value] || option.name || option.value;
  const name = String(option.name || option.value || fallback || '');
  return name.includes('/') ? name.slice(name.indexOf('/') + 1) : name;
}

/**
 * 优先用 ACP 的 configOptions（pi-acp 通过 session/set_config_option 支持
 * model 与 thought_level）；没有时退回旧的 models 字段。
 */
function setupConfigs(configOptions, models) {
  els.configs.innerHTML = '';

  const addSelect = ({ id, title, options, current, labeler }) => {
    if (!options.length) return;
    const select = document.createElement('select');
    select.className = 'select';
    select.title = title || id;
    for (const o of options) {
      const opt = document.createElement('option');
      opt.value = o.value;
      opt.textContent = labeler(o);
      if (o.description) opt.title = o.description;
      select.appendChild(opt);
    }
    if (current) select.value = current;
    select.addEventListener('change', () => {
      ws.send(JSON.stringify({ type: 'setConfig', configId: id, value: select.value }));
    });
    els.configs.appendChild(select);
  };

  if (Array.isArray(configOptions) && configOptions.length) {
    for (const co of configOptions) {
      if (!co || co.type !== 'select' || !Array.isArray(co.options)) continue;
      addSelect({
        id: co.id,
        title: co.description || co.name,
        options: co.options,
        current: co.currentValue,
        labeler: (o) => optionLabel(co.id, o),
      });
    }
    return;
  }

  if (!models || !Array.isArray(models.availableModels) || models.availableModels.length === 0) return;

  const groups = new Map();
  for (const m of models.availableModels) {
    const provider = String(m.modelId).includes('/') ? String(m.modelId).split('/')[0] : 'other';
    if (!groups.has(provider)) groups.set(provider, []);
    groups.get(provider).push(m);
  }

  const options = [];
  for (const [provider, list] of groups) {
    for (const m of list) {
      options.push({
        value: m.modelId,
        name: String(m.name || m.modelId).replace(`${provider}/`, ''),
        description: m.description,
      });
    }
  }
  addSelect({
    id: 'model',
    title: '模型',
    options,
    current: models.currentModelId,
    labeler: (o) => o.name,
  });
}

function showAuthCard({ message, methods }) {
  els.empty && els.empty.classList.add('hidden');
  const previous = document.getElementById('authCard');
  if (previous) previous.remove();

  const terminal = (methods || []).find((m) => m.type === 'terminal');
  const authMeta = terminal && terminal._meta && terminal._meta['terminal-auth'];
  const loginCommand = authMeta
    ? [authMeta.command, ...(authMeta.args || [])].join(' ')
    : 'pi-acp --terminal-login';

  const card = document.createElement('div');
  card.className = 'permission auth-card';
  card.id = 'authCard';
  card.innerHTML = `
    <div class="permission-title">pi 还没有可用的模型凭据</div>
    <div class="permission-desc"></div>
    <div class="auth-steps">
      <div>在终端里二选一完成登录：</div>
      <pre></pre>
      <div class="auth-hint">如果你给 pi 用了自定义配置目录，请确认 PI_CODING_AGENT_DIR 指向它；ACP 适配器会继承该环境变量。</div>
    </div>
    <div class="permission-actions">
      <button class="allow" id="authRetry">重新连接</button>
    </div>`;
  card.querySelector('.permission-desc').textContent = message || 'session/new 需要先完成认证。';
  card.querySelector('pre').textContent = `${loginCommand}\n# 或直接运行 pi，然后输入 /login`;
  card.querySelector('#authRetry').addEventListener('click', () => {
    card.remove();
    if (ws) ws.close();
  });
  els.messages.appendChild(card);
  scrollToEnd();
}

function onUpdate(u) {
  if (!u) return;
  switch (u.sessionUpdate) {
    case 'agent_message_chunk': {
      const text = u.content && u.content.type === 'text' ? u.content.text : '';
      if (!text) return;
      streamState().raw += text;
      scheduleRender();
      break;
    }
    case 'user_message_chunk': {
      const text = u.content && u.content.type === 'text' ? u.content.text : '';
      if (text) appendUser(text, true);
      break;
    }
    case 'agent_thought_chunk': {
      const text = u.content && u.content.type === 'text' ? u.content.text : '';
      if (!text) return;
      thoughtState().raw += text;
      thought.body.textContent = thought.raw;
      break;
    }
    case 'tool_call':
      closeBubble();
      createTool(u);
      break;
    case 'tool_call_update':
      updateTool(u);
      break;
    case 'plan':
      renderPlan(u.entries || []);
      break;
    case 'current_mode_update':
      break;
    case 'config_option_update':
      if (Array.isArray(u.configOptions)) setupConfigs(u.configOptions, null);
      break;
    case 'available_commands_update':
      break;
    default:
      break;
  }
}

function streamState() {
  if (!stream || stream.closed) {
    els.empty && els.empty.classList.add('hidden');
    const wrap = document.createElement('div');
    wrap.className = hasPrompted ? 'msg agent' : 'msg agent is-startup';
    const bubble = document.createElement('div');
    bubble.className = 'bubble';
    wrap.appendChild(bubble);
    els.messages.appendChild(wrap);
    stream = { el: wrap, bubble, raw: '', queued: false };
    thought = null;
  }
  return stream;
}

function thoughtState() {
  streamState();
  if (!thought) {
    const details = document.createElement('details');
    details.className = 'thought';
    const summary = document.createElement('summary');
    summary.textContent = '思考过程';
    const body = document.createElement('div');
    body.className = 'thought-body';
    details.append(summary, body);
    els.messages.insertBefore(details, stream.el);
    thought = { el: details, body, raw: '' };
  }
  return thought;
}

function scheduleRender() {
  if (stream.queued) return;
  stream.queued = true;
  requestAnimationFrame(() => {
    if (!stream) return;
    stream.queued = false;
    stream.bubble.innerHTML = renderMarkdown(stream.raw);
    scrollToEnd();
  });
}

function closeBubble() {
  if (stream) stream.closed = true;
  stream = null;
  thought = null;
}

function appendUser(text, fromAgent = false) {
  els.empty && els.empty.classList.add('hidden');
  const wrap = document.createElement('div');
  wrap.className = 'msg user';
  const bubble = document.createElement('div');
  bubble.className = 'bubble';
  bubble.textContent = text;
  wrap.appendChild(bubble);
  els.messages.appendChild(wrap);
  if (fromAgent) closeBubble();
  scrollToEnd();
}

function createTool(u) {
  const el = document.createElement('div');
  el.className = 'tool is-pending';
  el.dataset.id = u.toolCallId;
  el.innerHTML = `
    <div class="tool-head">
      <span class="tool-kind"></span>
      <span class="tool-title"></span>
      <span class="tool-status"></span>
    </div>
    <div class="tool-body hidden"><pre></pre></div>`;
  el.querySelector('.tool-kind').textContent = u.kind || 'tool';
  el.querySelector('.tool-head').addEventListener('click', () => {
    el.querySelector('.tool-body').classList.toggle('hidden');
  });
  els.messages.appendChild(el);
  updateTool(u, el);
}

function updateTool(u, existing) {
  const el = existing || els.messages.querySelector(`.tool[data-id="${cssEscape(u.toolCallId)}"]`);
  if (!el) return;

  if (u.title) el.querySelector('.tool-title').textContent = u.title;
  if (u.kind) el.querySelector('.tool-kind').textContent = u.kind;
  if (u.status) {
    el.className = `tool is-${u.status}`;
    el.querySelector('.tool-status').textContent = statusLabel(u.status);
  }

  const bodyEl = el.querySelector('.tool-body');
  if (u.rawInput) el.dataset.input = JSON.stringify(u.rawInput, null, 2);
  if (u.content) el.dataset.content = contentText(u.content);
  if (u.rawOutput) el.dataset.output = JSON.stringify(u.rawOutput, null, 2);

  const parts = [];
  if (el.dataset.input) parts.push(`input\n${el.dataset.input}`);
  if (el.dataset.content) parts.push(el.dataset.content);
  if (el.dataset.output) parts.push(`output\n${el.dataset.output}`);
  bodyEl.querySelector('pre').textContent = parts.join('\n\n') || '(暂无输出)';

  scrollToEnd();
}

function statusLabel(status) {
  return (
    { pending: '等待中', in_progress: '执行中', completed: '完成', failed: '失败', cancelled: '已取消' }[
      status
    ] || status
  );
}

function contentText(content) {
  const out = [];
  for (const c of content) {
    if (!c) continue;
    if (c.type === 'content' && c.content && c.content.type === 'text') {
      out.push(c.content.text);
    } else if (c.type === 'diff') {
      out.push(`diff ${c.path || ''}\n- ${(c.oldText || '').trim()}\n+ ${(c.newText || '').trim()}`);
    } else if (c.type === 'content') {
      out.push(`[${c.content && c.content.type}]`);
    } else {
      out.push(`[${c.type}]`);
    }
  }
  return out.join('\n\n');
}

function renderPlan(entries) {
  if (!entries.length) return;
  if (!planEl) {
    planEl = document.createElement('div');
    planEl.className = 'plan';
    planEl.innerHTML = '<div class="plan-title">计划</div>';
    els.messages.appendChild(planEl);
  }
  for (const old of planEl.querySelectorAll('.plan-item')) old.remove();
  for (const e of entries) {
    const row = document.createElement('div');
    row.className = `plan-item is-${e.status || 'pending'}`;
    const mark = document.createElement('span');
    mark.className = 'plan-mark';
    mark.textContent = e.status === 'completed' ? '✓' : e.status === 'in_progress' ? '›' : '·';
    const text = document.createElement('span');
    text.textContent = e.content;
    row.append(mark, text);
    planEl.appendChild(row);
  }
  scrollToEnd();
}

function showPermission({ id, params }) {
  const toolCall = params.toolCall || {};
  const card = document.createElement('div');
  card.className = 'permission';

  const title = document.createElement('div');
  title.className = 'permission-title';
  title.textContent = `请求授权：${toolCall.title || toolCall.kind || '工具调用'}`;

  const desc = document.createElement('div');
  desc.className = 'permission-desc';
  desc.textContent = toolCall.rawInput
    ? JSON.stringify(toolCall.rawInput, null, 2).slice(0, 600)
    : params.sessionId
      ? `session ${String(params.sessionId).slice(0, 8)}`
      : '';

  const actions = document.createElement('div');
  actions.className = 'permission-actions';

  const options = Array.isArray(params.options) && params.options.length
    ? params.options
    : [{ optionId: 'allow', name: '允许', kind: 'allow_once' }];

  for (const opt of options) {
    const btn = document.createElement('button');
    const isAllow = String(opt.kind || '').startsWith('allow');
    btn.className = isAllow ? 'allow' : 'deny';
    btn.textContent = opt.name || opt.optionId;
    btn.addEventListener('click', () => {
      ws.send(JSON.stringify({ type: 'permission', id, optionId: opt.optionId }));
      card.remove();
    });
    actions.appendChild(btn);
  }

  const cancel = document.createElement('button');
  cancel.textContent = '取消本轮';
  cancel.addEventListener('click', () => {
    ws.send(JSON.stringify({ type: 'permission', id, optionId: null }));
    card.remove();
  });
  actions.appendChild(cancel);

  card.append(title, desc, actions);
  els.permissionHost.appendChild(card);
}

function addSystem(text, isError = false) {
  const el = document.createElement('div');
  el.className = `system${isError ? ' error' : ''}`;
  el.textContent = text;
  els.messages.appendChild(el);
  scrollToEnd();
}

function clearConversation() {
  for (const el of els.messages.querySelectorAll('.msg, .tool, .plan, .system, .thought, .permission')) el.remove();
  planEl = null;
  stream = null;
  thought = null;
  hasPrompted = false;
}

function setBusy(value) {
  busy = value;
  els.send.classList.toggle('hidden', value);
  els.stop.classList.toggle('hidden', !value);
  els.input.disabled = false;
}

function scrollToEnd() {
  els.messages.scrollTop = els.messages.scrollHeight;
}

function send() {
  const text = els.input.value.trim();
  if (!text || !ready || busy) return;
  hasPrompted = true;
  closeBubble();
  appendUser(text);
  els.input.value = '';
  autoGrow();
  ws.send(JSON.stringify({ type: 'prompt', text }));
  setBusy(true);
}

function autoGrow() {
  els.input.style.height = 'auto';
  els.input.style.height = `${Math.min(els.input.scrollHeight, 200)}px`;
}

els.input.addEventListener('input', autoGrow);
els.input.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault();
    send();
  }
});
els.send.addEventListener('click', send);
els.stop.addEventListener('click', () => {
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'cancel' }));
  addSystem('已请求中断');
});
document.getElementById('newSession').addEventListener('click', () => {
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'newSession' }));
});
document.getElementById('toggleTraffic').addEventListener('click', () => {
  els.traffic.classList.toggle('hidden');
});
document.getElementById('closeTraffic').addEventListener('click', () => {
  els.traffic.classList.add('hidden');
});
});

function addTraffic({ dir, method, detail }) {
  if (els.traffic.classList.contains('hidden')) return;
  const row = document.createElement('div');
  row.className = `traffic-row ${dir}`;
  row.innerHTML = `<span class="dir">${dir === 'in' ? '<<' : '>>'}</span><span class="method"></span>`;
  row.querySelector('.method').textContent = method;
  if (detail) {
    const pre = document.createElement('pre');
    pre.textContent = JSON.stringify(detail, null, 2).slice(0, 900);
    row.appendChild(pre);
  }
  els.trafficBody.appendChild(row);
  if (els.trafficBody.childElementCount > 200) els.trafficBody.firstElementChild.remove();
  els.trafficBody.scrollTop = els.trafficBody.scrollHeight;
}

const ESCAPES = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
function escapeHtml(str) {
  return String(str).replace(/[&<>"']/g, (c) => ESCAPES[c]);
}

function inline(text) {
  return text
    .replace(/`([^`\n]+)`/g, '<code class="inline">$1</code>')
    .replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>');
}

function splitRow(line) {
  const cells = line.trim().replace(/^\|/, '').replace(/\|$/, '').split('|');
  return cells.map((c) => c.trim());
}

function renderText(text) {
  const lines = text.split('\n');
  let html = '';
  let list = null;
  let para = [];

  const flushPara = () => {
    if (!para.length) return;
    html += `<p>${inline(escapeHtml(para.join('\n'))).replace(/\n/g, '<br>')}</p>`;
    para = [];
  };
  const flushList = () => {
    if (!list) return;
    html += `<ul>${list.join('')}</ul>`;
    list = null;
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const item = line.match(/^\s*[-*]\s+(.*)$/);
    const heading = line.match(/^(#{1,4})\s+(.*)$/);
    const next = lines[i + 1] || '';
    const isTableStart = line.trim().startsWith('|') && /^\s*\|?[\s:|-]+\|?\s*$/.test(next) && next.includes('-');

    if (isTableStart) {
      flushList();
      flushPara();
      const head = splitRow(line);
      const rows = [];
      i += 2;
      while (i < lines.length && lines[i].trim().startsWith('|')) {
        rows.push(splitRow(lines[i]));
        i++;
      }
      i--;
      html += `<div class="table-wrap"><table><thead><tr>${head
        .map((c) => `<th>${inline(escapeHtml(c))}</th>`)
        .join('')}</tr></thead><tbody>${rows
        .map((r) => `<tr>${r.map((c) => `<td>${inline(escapeHtml(c))}</td>`).join('')}</tr>`)
        .join('')}</tbody></table></div>`;
      continue;
    }

    if (item) {
      flushPara();
      if (!list) list = [];
      list.push(`<li>${inline(escapeHtml(item[1]))}</li>`);
      continue;
    }
    flushList();
    if (heading) {
      flushPara();
      html += `<h3>${inline(escapeHtml(heading[2]))}</h3>`;
      continue;
    }
    if (!line.trim()) {
      flushPara();
      continue;
    }
    para.push(line);
  }
  flushList();
  flushPara();
  return html;
}

function renderMarkdown(src) {
  let out = '';
  let last = 0;
  const fence = /```([^\n`]*)\n([\s\S]*?)(?:```|$)/g;
  let match;
  while ((match = fence.exec(src))) {
    out += renderText(src.slice(last, match.index));
    out += `<pre><code>${escapeHtml(match[2].replace(/\n$/, ''))}</code></pre>`;
    last = fence.lastIndex;
  }
  out += renderText(src.slice(last));
  return out;
}

function cssEscape(value) {
  return String(value).replace(/["\\]/g, '\\$&');
}

connect();
