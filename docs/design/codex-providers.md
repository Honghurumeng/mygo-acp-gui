# Codex 自定义模型 / 供应商接入

## 背景

Codex 0.169 的 `WireApi` 只剩 `responses`，`wire_api = "chat"` 已直接报错；它自带的 `[model_providers.<id>]` 可以换 `base_url`，但上游必须会说 OpenAI Responses。除此之外，Codex 还要一份自己的模型目录（`model_catalog_json` 或 `model_catalog_url`），每个条目必须带 `base_instructions` 或 `model_messages.instructions_template`。

magpie 的做法是在 Codex 前面跑一个本地 Responses 网关，把请求翻译成 Chat Completions / Anthropic Messages / Gemini，同时生成 Codex 格式的模型目录。

## deepseek-recipe 是什么

[deepseek-ai/deepseek-recipe](https://github.com/deepseek-ai/deepseek-recipe) 是 DeepSeek 官方仓库（Rust workspace + Python bindings，MIT，0.2.0），但它**不是模式切换，也不是 Codex 网关**：

- 方向是“模型服务端”：把进来的 Chat Completions / Responses / Anthropic Messages 请求统一转成 `Conversation`，再渲染成 DeepSeek V4 / V4.1 的 prompt 和 token IDs；把后端产生的输出解析回对应协议的完整/流式响应。
- 调用方必须自己提供模型推理、工具执行和 HTTP 传输；库本身不发上游请求，也没有 `Conversation -> Chat Completions request` 的反向编码器。
- `ModelVersion` 只有 `DeepseekV4Pro` / `DeepseekV41Flash`，`with_model_alias` 只是给这两个版本起别名；prompt 渲染、special token、图片预处理（OpenCV）和 tokenizer 都是 DeepSeek 专用。
- 它适合“给 DeepSeek 模型套一层 OpenAI/Anthropic 兼容 API”，不适合“把 Codex 接到任意上游”。对 DeepSeek 自己来说也不需要它：DeepSeek 已原生提供 Responses 端点。

所以它最多作为 Responses 事件和输出解析的参考实现，不能当 Codex 的格式转换网关依赖。

## DeepSeek 官方路径

DeepSeek 现在不是“只能 Chat Completions”：

- Responses API：`base_url = https://api.deepseek.com`
- Anthropic API：`base_url = https://api.deepseek.com/anthropic`
- 官方 Codex 文档：`https://api-docs.deepseek.com/zh-cn/quick_start/agent_integrations/codex/`

官方一键配置脚本（`https://cdn.deepseek.com/api-docs/codex-deepseek-setup.sh`）不是协议转换器，而是配置管理器：

- 备份 `~/.codex/config.toml`
- 写入 `~/.codex/models.json`（Codex 模型目录，含 `deepseek-flash` / `deepseek-v4-pro`、reasoning levels、上下文、图片模态、`instructions_template`）
- 合并写入 `~/.codex/config.toml`：

```toml
model = "deepseek-flash"
model_provider = "deepseek"
preferred_auth_method = "apikey"
forced_login_method = "api"
model_catalog_json = "~/.codex/models.json"

[model_providers.deepseek]
name = "deepseek"
base_url = "https://api.deepseek.com/"
wire_api = "responses"
experimental_bearer_token = "<DeepSeek API Key>"
```

所以对 DeepSeek 自己来说，**不需要 magpie 的格式转换**；它已经同时提供了 Responses 端点和 Codex 模型目录。官方脚本可以做备份、合并、校验和恢复，这部分设计值得参考。

## 第三方“格式转换”项目

GitHub 上真正叫 format converter 的，是第三方项目，不是 DeepSeek 官方：

- `genevievehaltom3/deepseek-api-format-converter`
  - 496 行 Python，标准库 only，函数式接口：
    - `responses_to_chat`
    - `chat_to_responses`
    - `anthropic_to_chat`
    - `chat_to_anthropic`
    - `chat_stream_to_anthropic`
  - 有测试，字段丢弃会返回 warnings，代码干净、可直接读。
  - 但 **没有 `chat_stream_to_responses`**；`chat_to_responses` 只做非流式。Codex 实际是流式 + 工具调用 + reasoning 回放，这个库不能直接撑起 Codex agent loop。
  - README 明确写 “Not affiliated with DeepSeek”。

社区还有一批 Codex 专用小代理，补上了流式和工具循环，但不是官方：

- `yuercl/deepseek-codex-adapter`
- `2409324124/deepseek-codex-adapter`
- `FanFPu/deepseek-responses-bridge`
- `JetXu-LLM/codex-deepseek-bridge`
- `fish2lab/DSCodex`
- `7as0nch/mimo2codex`

它们适合当“最小 Responses ↔ Chat 代理”的参考实现；稳定性和维护程度不如 magpie。

## magpie 怎么处理

magpie 是通用网关，不是 DeepSeek 专用转换器：

- 内部 IR：`Request` / `Result` / `Event`
- 每协议 reader/encoder：`readResponses`、`readChat`、`readAnthropic`、`responsesEncoder`、`chatEncoder`、`anthropicEncoder`
- 入口：`/v1/responses`、`/backend-api/codex/responses`
- 上游原生支持 Responses 就 relay；否则 `translate()` 转成 Chat / Anthropic / Gemini，再把流翻译回来
- DeepSeek preset 同时有 Chat、Responses、Anthropic 三个地址；但 `Protocols` 顺序是 Chat 先，`ResponsesFirst` 对 DeepSeek 为 false，所以默认会先选 Chat，走 Responses → Chat 翻译，除非模型/用户明确指定 Responses

也就是说：DeepSeek 官方已经能直接说 Responses，magpie 仍然按通用 provider 逻辑处理，默认可能绕一圈走 Chat。它的价值在统一多 provider、账号池、路由、模型目录，而不是“DeepSeek 转换器”。

## 能干净拆出来吗

**不能。** 这不是一个独立库：

- 全部在 `internal/gateway`，Go 的 `internal` 包外部模块不能直接 import。
- `internal/gateway` 非测试代码约 13.5 万行；`internal/provider` 约 10.3 万行。
- 核心转换相关文件也已经约 9.3 千行：
  - `ir.go` ~1.1k
  - `responses.go` ~2.6k
  - `chat.go` ~1.9k
  - `anthropic.go` ~2.1k
  - `continuation.go` ~0.9k
  - `format.go` ~0.3k
- `translate()` / `streamTranslated()` 与 `Server`、账号池、重试、搜索、脱敏、usage、模型路由、缓存标记等耦合。
- IR 里带大量 magpie 自己的字段（Claude bridge、cache breakpoint、routing group 等）。

理论上可以“抠”出 IR + readers/encoders + SSE 一小套，但那是重写/裁剪，不是拿来做依赖。更现实的是把 `magpie serve` 当 sidecar 跑，或者写一个只服务 Codex 的小代理。

## 对我们这个 ACP GUI 的启发

`codex-acp` 2.6.0 bundle 里已经有 `providers/set`（unstable 扩展）：

```json
{
  "providerId": "openai",
  "apiType": "openai",
  "baseUrl": "http://127.0.0.1:3425/v1",
  "headers": {
    "Authorization": "Bearer <gateway-key>"
  }
}
```

它会把 `custom-gateway` 通过 `-c` 覆盖传给 `codex app-server`，重启 app-server，并请求 `<baseUrl>/models` 来**裁剪 Codex 已有的模型列表**。

关键限制：**`providers/set` 只裁剪，不新增模型目录条目。** 如果 Codex 的 catalog 里没有 `deepseek-flash`，`/models` 再返回它，Codex 的模型列表仍然是空的。要新增模型，必须另有 `model_catalog_json` / `model_catalog_url`（或用户已有的 `~/.codex/models.json`）。

### 已验证：`CODEX_CONFIG` 可以带完整 provider + 模型目录

`codex-acp` 启动时读 `CODEX_CONFIG`（JSON）。它的 `providerStartupConfig()` 会把 `model_provider` 和对应 `model_providers.<id>` 定义原样转成 `-c` 参数传给 `codex app-server`；provider 定义里的 `model_catalog_url` 也会一起传下去。`model` 不在启动参数里，但会进入 `thread/start` 的 config，因此可以作为会话默认模型。

```json
{
  "model_provider": "acp-gateway",
  "model": "demo",
  "model_providers": {
    "acp-gateway": {
      "name": "ACP Gateway",
      "base_url": "http://127.0.0.1:PORT/v1",
      "wire_api": "responses",
      "model_catalog_url": "http://127.0.0.1:PORT/models"
    }
  }
}
```

本机用 codex-acp 2.6.0 + Codex 0.169.0 实测：app-server 启动时请求 `GET /models?client_version=0.169.0`，`session/new` 返回网关目录里的 `demo` 模型；`model` 也设成 `demo` 时，`currentModelId` 是 `demo[medium]`。整个过程只读进程环境，不写 `~/.codex`。

注意：目录在 app-server 启动时加载，改 provider / catalog / 默认模型需要重启 agent；`providers/set` 会用自己的 provider 定义覆盖 `CODEX_CONFIG` 里的定义（且不带 `model_catalog_url`），两者不要混用。

### 已实现的 GUI 设置层（Phase 1）

`docs/design/codex.md` 的 `Codex` 设置分类已经落地：

- `settings.json` 增加 `codex:{override,provider:{id,name,baseUrl,apiKeyEnv,apiKey,useCodexLogin,modelCatalogUrl,model,contextWindow,reasoningLevels}}`；provider id 规范化为 `[a-z0-9_-]+`，缺省 `acp-gui`。
- `internal/agent.WithCodexOverride` 从**发现的 base spec** 重建 Codex launch spec，把覆盖写进 `CODEX_CONFIG`，并保留用户已有的其他 `CODEX_CONFIG` key。关闭开关时重新用 base spec，不会残留上一次的 `CODEX_CONFIG`。
- 设置面板支持 API key 的**环境变量名**或**直接 key**；直接 key 存 `settings.json`（0600），启动时注入子进程环境，不写 `CODEX_CONFIG`/日志；`Use Codex login` 走 `requires_openai_auth`。`base_url` 自动补 `/v1`。
- 只影响本应用启动的 Codex 会话，不写 `~/.codex`；其他 Codex 设置继续由 Codex 读取。
- 点 Apply 后刷新 Codex 的配置探测；新会话生效，已连接会话需要重连。

### 已实现：生成的本地模型目录（Phase 2）

`settings.json` 增加 `codex.provider.contextWindow` 和 `codex.provider.reasoningLevels`，并在用户没有填 `modelCatalogUrl` 时自动生成 Codex 模型目录：

- `internal/agent/codex_catalog.go` 启动一个只监听 `127.0.0.1` 随机端口的目录服务，路径 `/models`，返回 `{"models":[...]}`；Codex 追加的 `?client_version=` 被忽略。服务随 App 生命周期运行，`Shutdown` 时关闭。
- 目录条目由设置生成：`slug` 用 model，`display_name` 用 model，`context_window` / `max_context_window` 用设置值（缺省 200000），`supported_reasoning_levels` / `default_reasoning_level` 用设置值（缺省 `low,medium,high` + `medium`）。
- 条目必须带 `base_instructions`。`internal/agent/codex_prompt.txt` 内嵌 Codex 的 coding prompt，避免自定义模型拿到空系统提示词。
- 用户填了 `Model catalog URL` 时完全使用用户的 URL，不启动/不替换本地目录。
- 点 Apply 后重建 launch spec，把 `model_catalog_url` 放进 provider 定义，仍通过 `CODEX_CONFIG` 注入；不写 `~/.codex/config.toml`。

实测（codex-acp 2.6.0 + Codex 0.169.0）：`session/new` 返回 `currentModelId = deepseek/deepseek-v4.1-flash[medium]`，`configOptions` 里出现 `reasoning_effort`（`category = thought_level`），不再打印 `Model metadata for ... not found`。

Codex 0.169 的目录解析是整份生效的，缺一个必填字段会让整份目录变空，`session/new` 报 `Codex did not return any models`。本机验证的最小必填集：`slug`、`display_name`、`shell_type`、`visibility`、`supported_in_api`、`priority`、`support_verbosity`、`truncation_policy`、`experimental_supported_tools`、`base_instructions`（或 `model_messages.instructions_template`）。`context_window`、`description`、`default_verbosity` 等可选；不填 context 时 Codex 会用 fallback 限制。

注意：Codex 会把拉到的目录写进 `<CODEX_HOME>/models_cache.json`（默认 `~/.codex/`，只写缓存，不改 config）。每次 app-server 启动都会重新拉取；如果本地目录无效，Codex 会退回上一次的缓存而不是报目录错误，所以调试时先看 `models_cache.json` 和 ACP 的模型菜单。

当前仍然是单 model：目录只包含设置里的那个 model。多 model 切换还需要在设置里维护列表，并扩展 `CodexCatalogJSON` 的入参；目录服务已经能直接返回多条。自定义 `http_headers`、多 provider 也还没有 UI。

这一步仍然不需要协议转换；等 provider 是 Chat-only 时，再考虑 Responses 网关。

### 方案对比

| 方案 | 工作量 | 能力 |
| --- | --- | --- |
| 只用 DeepSeek：原生 Responses + 官方 `models.json` / `CODEX_CONFIG` | 小 | 无需代理；模型目录必需 |
| 上游本身说 Responses：`CODEX_CONFIG` 或 `providers/set` | 小 | 只换路由；任意模型仍需目录 |
| 外部 magpie：用户跑 `magpie serve`，我们用 `CODEX_CONFIG` 指过去 | 中 | 协议转换现成；目录仍需 magpie 提供或我们注入 |
| 自建 Responses 网关 | 大 | 任意 Chat / Anthropic / Gemini 上游；要自己做协议转换 + catalog + SSE/工具/reasoning |

**结论**：给 Codex 设任意供应商是可行的，但 Codex 只认 Responses，所以“任意供应商”必然意味着自建一个本地 Responses 网关；DeepSeek 官方路径和 deepseek-recipe 都解决不了 Chat-only 上游。

网关最小职责：

1. `GET /models` 返回 Codex 的完整 `{"models":[...]}` 目录（不是 OpenAI `/v1/models` 的 `{"data":[...]}`）。
2. `POST /v1/responses` 把 Responses 请求翻译成上游 Chat Completions / Anthropic / Gemini，并把上游流翻译回 Responses SSE 事件（文本、reasoning、function call、usage、错误、取消）。
3. 维护 catalog `slug -> provider/model` 映射；Codex 只把 slug 发给网关。
4. 上游 key 留在网关进程，Codex 只拿本地随机 token，避免 key 进入 Codex 配置或 `ps` 参数。

第一版建议只做 OpenAI Chat Completions 上游 + 文本/流式/function tools/reasoning；Anthropic/Gemini 后加。SSE 事件顺序和 tool call id 映射是主要坑，magpie 的 `internal/gateway` 可当参考，但不能直接 import。

`providers/set` 仍然适合“上游已经是 Responses、只需换 base_url”的场景；它的 `apiType` 目前只有 `openai -> responses`，provider 定义里没有 `model_catalog_url`，所以只能裁剪已有模型，不能新增。

## 几个容易踩的坑

- Codex 0.169 的 `wire_api` 只接受 `"responses"`，`"chat"` 已被移除。
- `model_catalog_json` / `model_catalog_url` 都是 app-server 启动时读的，改了要重启/重连 Codex。
- 目录里每个模型都必须带 `base_instructions` 或 `model_messages.instructions_template`，否则整份目录作废；`support_verbosity`、`truncation_policy`、`experimental_supported_tools` 也是必填，缺一个就没有模型。当前 `ModelInfo` 还有 `support_verbosity`、`truncation_policy`、`availability_nux`、`supported_reasoning_levels` 等字段，最好拿一份已知可用的 catalog 当模板改 `slug`/显示名/上下文。
- Codex 会把 `model_catalog_url` 的响应缓存到 `<CODEX_HOME>/models_cache.json`。目录无效时会静默沿用旧缓存；改了设置但菜单没变，先删缓存或换一个 `CODEX_HOME` 验证。
- `reasoningLevels` 为空时目录不声明 reasoning，Codex 不显示“推理强度”，也不会在请求里带 `reasoning`；填了未知 level（如 `turbo`）会在 Apply 时报错，而不是把整份目录弄坏。
- `model_catalog_url` 是完整 URL，Codex 会追加 `?client_version=0.169.0`；返回 Codex 的 `{"models":[...]}`，不是 OpenAI 的 `/v1/models`。
- `CODEX_CONFIG` 只在 agent 启动时读；`codex-acp` 只转发 provider 定义，不转发顶层 `model_catalog_json`。要走文件目录，得改 bundle 或自己起 app-server；更稳的是让网关提供 `model_catalog_url`。
- Codex 发给网关的是 catalog 里的 `slug`，网关必须能把它映射回真实的 `provider/model`。
- `providers/set` 的 `/models` 只用于裁剪已有模型，不能替代 catalog；空交集会让 Codex 一个模型都没有。
- `providers/set` 会覆盖 `CODEX_CONFIG` 的 provider 定义并重启 app-server，`CODEX_CONFIG` 里的 `model_catalog_url` 会丢失。

## 参考

- DeepSeek Responses API：`https://api-docs.deepseek.com/zh-cn/guides/responses_api/`
- DeepSeek Codex 接入：`https://api-docs.deepseek.com/zh-cn/quick_start/agent_integrations/codex/`
- deepseek-recipe（官方，服务端方向）：`https://github.com/deepseek-ai/deepseek-recipe`
- 第三方纯转换器：`https://github.com/genevievehaltom3/deepseek-api-format-converter`
- magpie 写 Codex 配置：`internal/agent/codex.go`、`internal/agent/codex_profile.go`
- magpie 生成模型目录：`internal/codexcat/codexcat.go`
- magpie 的 Codex 入口：`internal/gateway/codex_backend.go`
- magpie 的协议翻译：`internal/gateway/gateway.go`（`translate()`）
- Codex 协议限制：`codex-rs/model-provider-info/src/lib.rs`（`WireApi`）
- Codex 目录要求：`codex-rs/protocol/src/openai_models.rs`（`ModelsResponse` / `base_instructions`）
