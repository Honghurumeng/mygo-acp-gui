# acp-pi-gui

一个最小可用的 **ACP（Agent Client Protocol）GUI 客户端**，通过 [`@geohar/pi-acp`](https://www.npmjs.com/package/@geohar/pi-acp) 驱动 [pi](https://pi.dev) 编码 agent。

浏览器里聊天，后端把 ACP 的 JSON-RPC 报文转发给 stdio 上的 pi 适配器。

```
┌─────────────┐   WebSocket   ┌──────────────┐   JSON-RPC   ┌──────────────┐
│  浏览器 UI  │ ◄───────────► │ Node 服务端  │ ◄──────────► │  pi-acp      │
│  public/    │   (JSON)      │ server/      │  over stdio  │  ↓           │
└─────────────┘               └──────────────┘              │  pi --mode rpc│
                                                            └──────────────┘
```

## 快速开始

```bash
cd acp-pi-gui
npm install
npm start
# 打开 http://127.0.0.1:5178
```

前置条件：

- Node.js 22+
- `pi` 已安装并在 PATH 上（`npm i -g @earendil-works/pi-coding-agent`，要求 ≥ 0.80.4）
- pi 已经配置好模型凭据（终端里跑一次 `pi`，`/login` 或用 API key）

工作目录默认是启动服务时所在的目录，可以指定：

```bash
ACP_WORKSPACE=/path/to/your/repo npm start
# 或 http://127.0.0.1:5178/?cwd=/path/to/your/repo
```

## 功能

- 流式输出：`agent_message_chunk` 逐块渲染 Markdown（代码块 / 表格 / 列表 / 行内代码）
- 思考过程：`agent_thought_chunk` 折叠展示
- 工具调用：`tool_call` / `tool_call_update` 实时状态卡片（pending → in_progress → completed/failed），可展开看入参与输出
- 权限审批：`session/request_permission` 弹出卡片，按 option 允许/拒绝
- 计划列表：`plan` 更新渲染
- 模型切换：从 `session/new` 返回的 models 里选，先试 `session/set_model`，失败回退 `session/set_config_option`
- 中断：`session/cancel`
- 报文监视器：右上角「报文」可看双向 ACP 流量，排查适配器问题很有用

## 目录结构

```
server/
  index.mjs        HTTP 静态服务 + WebSocket + 会话编排
  acp-client.mjs   ACP 客户端：JSON-RPC over stdio、请求/通知分发、权限回执
public/
  index.html       页面骨架
  app.js           WS 协议、消息渲染、Markdown、权限 UI
  style.css        样式
scripts/
  smoke.mjs        无浏览器的完整链路冒烟（initialize → session/new → prompt）
  ws-test.mjs      只测 Web 层：像浏览器一样连 WS 并发一轮 prompt
```

## UI 行为 ↔ ACP 方法

| 界面动作 | ACP |
| --- | --- |
| 建立连接 | `initialize`（声明 `clientCapabilities`）→ `session/new` |
| 发送消息 | `session/prompt` |
| 流式渲染 | 通知 `session/update` |
| 授权弹窗 | agent 发起 `session/request_permission`，客户端回 `{ outcome: { outcome: 'selected', optionId } }` |
| 停止 | 通知 `session/cancel` |
| 切换模型 | `session/set_model`，回退 `session/set_config_option` |
| 新会话 | 再发一次 `session/new` |

### 为什么不开 fs / terminal 能力

`initialize` 里声明的是：

```js
clientCapabilities: {
  fs: { readTextFile: false, writeTextFile: false },
  terminal: false,
}
```

pi 自己的 read / write / edit / bash 工具已经能读写工作区，不需要客户端代劳。关掉这两个能力后，文件与命令的执行完全留在 pi 进程内，客户端只负责展示和审批——这也避免了「沙箱里再套一层沙箱」的权限混乱。如果以后要做「所有写操作必须经编辑器落盘」（比如走编辑器 buffer 而不是磁盘），再把能力打开并实现 `fs/*`、`terminal/*` 即可。

## 环境变量

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `PORT` | `5178` | 监听端口 |
| `HOST` | `127.0.0.1` | 监听地址 |
| `ACP_WORKSPACE` | `process.cwd()` | 默认会话 cwd |
| `ACP_AGENT_CMD` | `npx` | 适配器命令 |
| `ACP_AGENT_ARGS` | `-y @geohar/pi-acp` | 适配器参数（空格分隔） |
| `PI_CODING_AGENT_DIR` | pi 自己的解析 | 透传给 pi；用自定义配置目录时必设 |

## 排查

**「pi 还没有可用的模型凭据」** — 适配器在 `session/new` 返回 `-32000`，说明 pi 侧没有可用凭据。终端里跑 `pi` 然后 `/login`。

**pi 报 `Lock file is already being held` / `Invalid settings file`** — `~/.pi/agent/*.lock` 有残留空目录，pi 因此读不到 `settings.json`（进而找不到 key）。清掉即可：

```bash
rmdir ~/.pi/agent/*.lock
```

**想省掉启动信息块** — pi-acp 会把 pi 版本、skills、prompts 之类的启动信息塞进会话。在 `~/.pi/agent/settings.json` 里设 `"quietStartup": true` 可关掉（UI 这边把首轮提问前的输出统一渲染成灰色小方块，不会和回答混在一起）。

**首屏一直「正在启动」** — `npx` 首次拉包会慢几秒；之后有缓存就快了。想更快可以 `npm i -g @geohar/pi-acp`，然后 `ACP_AGENT_CMD=pi-acp ACP_AGENT_ARGS= npm start`。

## 还没做

- `session/load`（续聊历史会话）——适配器支持，客户端侧需要加会话列表 UI
- 图片/资源块输入（适配器声明了 `promptCapabilities.image`）
- `session/set_mode` 模式切换、多会话并行
- 中断后对未完成 `tool_call` 的状态收尾
