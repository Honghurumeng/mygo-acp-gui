# 单进程多会话调查

调查时间：2026-11-13。对象：ACP 协议、Command Code `1.88.5`（本机 `command-code acp`）、vendored pi-acp `0.4.0` fork（`resources/agents/pi-acp/pi-acp.bundle.mjs`）。结论来自静态检查 CLI / bundle 源码，未做运行时并发会话测试。

## 结论

| 层级 | 一个进程多个 ACP 会话 | 限制 |
| --- | --- | --- |
| ACP 协议 / SDK | 支持 | 每个请求自带 `sessionId`，协议本身不限制数量 |
| Command Code 1.88.5 | 支持，可并发 prompt | 所有活动会话必须共用同一个 cwd；换工作区要新起进程 |
| vendored pi-acp 0.4.0 fork | 不支持并发 | `session/new` / `load` / `resume` 会关闭此前所有会话，只保留最新一个 |
| 当前 GUI | 每会话独立进程 | 见 [features.md](features.md#多会话)，不依赖 agent 的单进程多会话能力 |

## ACP 协议

- `session/new` 每次返回新的 `sessionId`，之后的 `session/prompt`、`session/cancel`、`session/update`、`session/request_permission` 都用它区分会话。
- SDK 的 `AgentSideConnection` 只是把每次 `session/new` 交给 agent 的 `newSession()`，没有“一个连接只能一个会话”的约束。
- 多会话能否并行，取决于 agent 实现；客户端不应假设一定支持。

## Command Code

`command-code/dist/cli.mjs` 的 `createAcpServer()`：

- 用 `Map<sessionId, session>`（`n`）保存活动会话；`openSession()` 创建 agent session 后 `n.set(id, session)`，`handleNewSession()` 不清空已有会话，`handleCloseSession()` 只删指定 id。
- 每个会话有独立的 `agent`、`prompting`、`activeTurn`、权限集合和 usage；`handlePrompt()` 只检查该会话自己的 `prompting`，所以不同会话可以同时跑 prompt。
- cwd 由模块级 `createCwdLease()` 约束：同一时刻只允许一个规范化后的 cwd，但允许多个会话共享它（引用计数）。跨 cwd 创建/加载会返回 `-32602`：

  ```text
  This cmd acp process serves <heldCwd>; start another for <cwd>
  ```

  所有会话关闭后 lease 归零，进程可以换到新 cwd。已有 session id 在 `attachSession()` 里还会校验 load 的 cwd 必须与打开时一致。
- `session/list` 由 CLI 自己扫描磁盘，与活动会话无关；`session/close` 会 dispose 该会话的 agent。

因此同一 workspace 可以在一个 `command-code acp` 进程里开多个会话；跨 workspace 必须另起进程。这与 [command-code.md](command-code.md) 中“进程只服务一个工作区”的现有记录一致。

## pi-acp（vendored 0.4.0 fork）

`pi-acp.bundle.mjs` 的 `PiAcpAgent`：

- `sessions = new SessionManager`，内部 `sessions = new Map`；每个会话各自 spawn 一个 `pi --mode rpc` 子进程（`PiRpcProcess`），持有独立的 pi 会话文件和 cwd。
- 但 `newSession()`、`loadSession()`、`resumeSession()` 在创建/恢复目标会话后都会调用：

  ```js
  this.sessions.closeAllExcept(session.sessionId, preexistingSessionIds)
  ```

  `preexistingSessionIds` 是调用前的会话列表，所以结果是：**新会话保留，此前所有会话被 dispose 并从 Map 删除**。
- `session/close` 可以单独关闭指定会话（`this.sessions.close(id)`）；`dispose()` 关闭全部。
- `lastSessionCwd` 是进程级字段，会被最近一次 `new/load/resume` 改写；只有 `session/list` 省略 cwd 时才作为默认过滤条件，不影响已有会话的 cwd。

实际语义是“可以反复创建会话，但同一进程同时只有一个活会话”：第二次 `session/new` 会让第一个会话的 pi 子进程退出，之后只能靠 `session/load` 重新拉起。pi 的会话文件仍保存在磁盘上，因此历史不会丢。

## 对 GUI 的影响

- 当前实现是每个 `Chat` 执行一次 `session.Start`，即每个会话一个 agent 进程；上面的限制不改变现有行为。
- 若未来想共享进程：
  - Command Code：可按 workspace 复用进程（同一 cwd），跨 workspace 继续新建；需要把进程按路由归属到会话，并处理一个进程内多会话的权限/取消/usage 分发。
  - pi：需要修改 pi-acp（去掉或放宽 `closeAllExcept`）才能并发；否则共享进程只能串行切会话，收益有限。
- 不要仅凭协议支持就假设 pi 能并发；以 vendored bundle 的实际行为为准。

## 复核方法

```bash
# Command Code：查 createAcpServer / openSession / createCwdLease
python3 - <<'PY'
import re
s=open('/Users/reed/.npm-global/lib/node_modules/command-code/dist/cli.mjs').read()
for pat in ['createAcpServer','createCwdLease','handleNewSession']:
    print(pat, [m.start() for m in re.finditer(pat,s)])
PY

# pi-acp：查 SessionManager / closeAllExcept
python3 - <<'PY'
import re
s=open('resources/agents/pi-acp/pi-acp.bundle.mjs').read()
for pat in ['SessionManager','closeAllExcept','async newSession']:
    print(pat, [m.start() for m in re.finditer(pat,s)])
PY
```
