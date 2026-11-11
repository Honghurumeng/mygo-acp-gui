# pi-acp fork 修改清单

目标：fork `https://github.com/georgeharker/pi-acp`（当前 vendored 0.4.0 的来源），让 pi 通过 ACP 发出上下文用量。Go 客户端不改，仍然只运行 vendored bundle，不引入运行时 npm/npx。

## 1. fork 源码

### 必需：上下文用量

`src/acp/session.ts`（`PiAcpSession`，放在 `emit` / `flushEmits` 附近）：

```ts
async emitContextUsage(): Promise<void> {
  try {
    const stats = (await this.proc.getSessionStats()) as {
      contextUsage?: { tokens?: number | null; contextWindow?: number | null }
    }
    const used = stats?.contextUsage?.tokens
    const size = stats?.contextUsage?.contextWindow
    if (typeof used !== 'number' || typeof size !== 'number' || used < 0 || size <= 0) return
    this.emit({
      sessionUpdate: 'usage_update',
      used: Math.trunc(used),
      size: Math.trunc(size)
    })
    await this.flushEmits()
  } catch {
    // stats 是可选的；失败不影响对话
  }
}
```

`src/acp/agent.ts`（`prompt()` 普通路径，`await session.prompt(message, images)` 之后）：

```ts
const result = await session.prompt(message, images)
await session.emitContextUsage()
```

每个 turn 结束发一次即可；不要在每次 `message_update` 都查 stats。`contextUsage` 为 `null` 时跳过（压缩后可能短暂为空）。

### 可选：单轮 usage

本项目 UI 已支持 `PromptResponse.usage`，显示为 `turn 1.2k in · 100 out`。

- `src/acp/session.ts`：`handlePiEvent` 的 `message_update` 读取 `ev.usage`；把 pi 的 `input` / `output` / `cacheRead` / `cacheWrite` / `totalTokens` 映射为 ACP 的 `inputTokens` / `outputTokens` / `cachedReadTokens` / `cachedWriteTokens` / `totalTokens`。`startTurn()` 清空，提供 `consumeTurnUsage()`。
- `src/acp/agent.ts`：有有效 usage 时返回 `{ stopReason, usage }`，否则保持 `{ stopReason }`。

## 2. 本仓库 vendor 接入

`scripts/vendor-pi-acp.sh` 目前写死 `@geohar/pi-acp`。增加两个变量：

```bash
PI_ACP_PACKAGE="${PI_ACP_PACKAGE:-@geohar/pi-acp}"
PI_ACP_SOURCE="${PI_ACP_SOURCE:-$PI_ACP_PACKAGE@$VERSION}"
```

替换所有硬编码处：`npm install`、`pkg_version`、esbuild 入口、LICENSE 路径、生成的 bundle 旁 `package.json`、`THIRD_PARTY_NOTICES.md`。

本地 tarball 接入：

```bash
(cd ../pi-acp && npm ci && npm run build && npm pack --pack-destination /tmp)
PI_ACP_PACKAGE=@yourscope/pi-acp \
PI_ACP_SOURCE=/tmp/yourscope-pi-acp-0.4.0.tgz \
PI_ACP_VERSION=0.4.0 \
scripts/vendor-pi-acp.sh
```

或发布到自己的 npm scope 后用 `PI_ACP_SOURCE=@yourscope/pi-acp@版本`。不要手改 `resources/agents/pi-acp/pi-acp.bundle.mjs`；bundle、`VERSION`、`package.json`、`SHA256SUMS` 都由脚本重新生成。

## 3. 验证

```bash
node scripts/pi-acp-smoke.mjs
ACP_REAL_PI=1 go test ./internal/session -run TestRealPi -v -count=1
go test ./... -race
```

真实 pi 会话中确认会话栏出现 `context x / y`（可选 `turn …`），并检查 `logs/pi.log` 里没有协议错误。

## 4. 注意事项

- `$VERSION` 用于校验解析后的包版本；本地 fork 版本号不是 0.4.0 时同步修改，或放宽脚本中的版本断言。
- 保持 `@agentclientprotocol/sdk@0.26.0`、`zod@3.25.76` 不变；升级 SDK 属于独立变更。
- bundle 旁的 `package.json` 决定 `agentInfo.name/version`；包名改了会显示在新状态行/日志里，但 Agent 下拉仍是内置的 pi。
- 保留 pi-acp 的 MIT `LICENSE` 和 `THIRD_PARTY_NOTICES.md`；修改处建议在 fork 的 commit/README 注明。
- 长期维护要 rebase 上游；若 `usage_update` 映射是通用需求，优先给上游提 PR。
- 接入后同步更新 `docs/design/pi.md` 的版本表、sha256、来源和用量说明。
