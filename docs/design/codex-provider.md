# Codex 自定义供应商

## 目标与边界

设置面板提供一份可选的 OpenAI-compatible provider，让 Codex 不必依赖用户 `~/.codex/config.toml` 里已配置的 provider。它覆盖五件事：

1. 指定 provider 名称、Base URL、API key 和模型列表。
2. 让 Codex 接受不在 Codex 模型目录里的 model id，并在 composer 的模型面板里可选；provider 开启时同时生成这些 id 的 Codex model catalog，避免 fallback metadata。
3. 设置模型的完整上下文大小和自动压缩触发比例，并同时写入 Codex 配置与生成的 catalog。
4. 用复选框选择这些自定义模型的推理档位（None / Minimal / Low / Medium / High / Xhigh / Max / Ultra），不再固定只有 `medium`。
5. 用设置里的标题模型（或会话当前模型）生成会话标题，不再固定请求 `gpt-5.6-luna`。

模型列表、上下文大小、压缩比例、推理档位和标题模型与 provider 路由开关解耦：开关关闭时 `MODEL_PROVIDER`/API key 不设置，Codex 继续用 `~/.codex/config.toml` 的 provider；但上下文/压缩会通过 `CODEX_CONFIG` 生效，`CODEX_ACP_MODELS`/`CODEX_ACP_REASONING_EFFORTS`/`CODEX_ACP_TITLE_MODEL` 也仍会生效，因此可以只扩展现有 provider 的模型目录与限制，而不覆盖它的 Base URL。

只支持一种 provider、一个 Base URL；不写 `~/.codex/config.toml`，不读取其中的凭据，不做登录，不代理请求，也不在运行时自动发现 provider 的 `/models`。Codex 目前只支持 `wire_api = "responses"`；需要 Chat Completions 的供应商必须自带 Responses 兼容层。

## 数据流

```
settings.json (codexProvider)
  → internal/codexprovider.Config
  → internal/agent.CodexLaunchSpec
      CODEX_CONFIG / MODEL_PROVIDER / ACP_GUI_CODEX_PROVIDER_API_KEY
      CODEX_CONFIG.model_context_window / model_auto_compact_token_limit
      CODEX_CONFIG.model_catalog_json（<data>/codex-models.json）
      CODEX_ACP_MODELS / CODEX_ACP_REASONING_EFFORTS / CODEX_ACP_TITLE_MODEL
  → codex-acp bundle (Node)
  → codex app-server (CODEX_PATH)
```

`CodexLaunchSpec` 每次都用 `Environment.Env`（登录 shell 的干净副本）重新构造子进程环境，所以保存设置后重建 spec 不会累积旧变量。provider 禁用时不添加 provider 路由变量，只保留 model/context/reasoning/title 覆盖；没有覆盖时 Codex 完全回到用户自己的配置。

## 设置字段

`settings.json` 的 `codexProvider`：

```json
{
  "enabled": true,
  "name": "DeepSeek",
  "baseURL": "https://api.example.com/v1",
  "apiKey": "sk-...",
  "models": ["deepseek-chat", "deepseek-reasoner"],
  "contextWindow": 1048576,
  "autoCompactPercent": 80,
  "reasoningEfforts": ["low", "medium", "high"],
  "titleModel": "deepseek-chat"
}
```

- `enabled`：控制是否覆盖 Codex provider 路由；关闭时 Base URL/name/key 不传给 Codex，但 `models`/`contextWindow`/`autoCompactPercent`/`reasoningEfforts`/`titleModel` 仍作为适配器覆盖生效。
- `name`：Codex `model_providers.acp-gui.name`；空值在启用时补 `Custom Codex provider`。
- `baseURL`：必须是 http(s)、有 host、不能带 URL 内嵌凭据；保存时去掉末尾 `/`。
- `apiKey`：可选；写入 settings.json（0600），通过环境变量传给 Codex，不出现在命令行。
- `models`：一行一个；trim、去空、去重。全部追加到 ACP `model` 选项；`enabled` 时第一个还是 `CODEX_CONFIG.model`（新会话默认模型），每个 id 也会成为生成的 Codex catalog 条目。空列表时只用 Codex 自己的目录。
- `contextWindow`：完整模型上下文大小，单位 token；界面允许 1,024 到 100,000,000，空值表示不覆盖 Codex/catalog 默认。非空时写入 `CODEX_CONFIG.model_context_window`，生成的 DeepSeek catalog 同时覆盖 `context_window` 和 `max_context_window`。Codex 仍按模板里的 `effective_context_window_percent: 95` 预留 5% headroom，所以上下文环显示的是可用大小，不是完整值。
- `autoCompactPercent`：自动压缩触发点占完整 context window 的百分比，范围 1% 到 90%；设置它时必须同时设置 `contextWindow`。界面和持久化用百分比，生成 Codex 配置时换算为 `floor(contextWindow * percent / 100)`，分别写入 `CODEX_CONFIG.model_auto_compact_token_limit` 和 catalog 的 `auto_compact_token_limit`。90% 是 Codex 主自动压缩的硬上限；它不是“压缩后只保留原上下文多少”的比例。
- `reasoningEfforts`：可选；模型设置里的复选框决定这些自定义模型的 Thinking 档位，对应 `CODEX_ACP_REASONING_EFFORTS`。为空或旧文件没有该字段时仍只有 `medium`。保存时按 `none, minimal, low, medium, high, xhigh, max, ultra` 的顺序写入，并至少勾选一项。`medium` 被勾选时作为自定义模型的默认档位；否则第一个勾选项作为默认。Codex 目录里已有的同名模型仍使用它自己的档位，不受这里的勾选影响。
- `titleModel`：可选；对应 `CODEX_ACP_TITLE_MODEL`，无论 `enabled` 是否开启都生效。空值时标题跟随会话当前模型；再从适配器里回退到 `gpt-5.6-luna`。

设置面板在打开时把已保存值复制到 `CodexProviderDraft`，并把 `EffortLevels()` 映射到 `CodexProviderEfforts` 复选框；context 和 compact 在编辑期间保留为字符串，空值不等于 0，而是“不覆盖默认”。只有点击 `Save provider` 才解析数字、校验、落盘并重建 Codex launch spec。URL、模型 id、上下文范围、压缩范围非法，只设置压缩比例却没设上下文，或一个推理档位都没勾选时显示 `⚠` 错误且不保存。输入允许逗号/下划线分隔的数字。关闭再打开会丢弃未保存的编辑。

## 生成的 Codex 配置

按需写入子进程环境：

| 变量 | 条件 | 值 |
| --- | --- | --- |
| `MODEL_PROVIDER` | `enabled` | `acp-gui` |
| `CODEX_CONFIG` | `enabled` 或设置了上下文/压缩 | `enabled` 时含 `model_provider=acp-gui`、可选 `model=<第一个模型>`、`model_catalog_json=<data>/codex-models.json`、`model_providers.acp-gui={name,base_url,wire_api:"responses",requires_openai_auth:false,env_key?}`；设置后另含 `model_context_window` 和 `model_auto_compact_token_limit` |
| `ACP_GUI_CODEX_PROVIDER_API_KEY` | `enabled` 且有 key | 用户填写的 API key |
| `CODEX_ACP_MODELS` | 有模型 | 模型 id 的 JSON 数组 |
| `CODEX_ACP_REASONING_EFFORTS` | 有模型 | 勾选档位的 JSON 数组；未配置时为 `["medium"]` |
| `CODEX_ACP_TITLE_MODEL` | 有标题模型 | 标题模型 |

codex-acp 原有的 `providerStartupConfig` 把 `model_provider` 和 provider 定义做成 `codex app-server` 的 `-c` 参数；`createSessionConfig` 把 `model` 等配置带进 `thread/start`。`CODEX_ACP_*` 是本仓库补丁读取的适配器变量，不会传给 Codex CLI。

## 模型与标题

- `CustomModels.ts` 把 `CODEX_ACP_MODELS` 里的 id 合并在 Codex `model/list` 之后；同名时 Codex 目录条目优先。`CODEX_CONFIG.model` 也会自动补进列表，所以 provider 的当前模型切换走后仍能选回。合成条目使用 `text` 输入、`CODEX_ACP_REASONING_EFFORTS` 的档位列表和 `isDefault=false`，用于 ACP 列表；provider 开启时另由 Go 侧生成 catalog（见下节），Codex 使用其真实 metadata。该变量缺失或非法时退回单一的 `medium`。`medium` 存在时标为 `Default`，否则第一个档位标为 `Default`。会话从 Codex 配置带入的 effort 不在声明列表时，适配器保留模型并回退到默认档位，使 `reasoning_effort.currentValue` 始终落在选项里。
- 因为合成模型已经在 `sessionState.availableModels` 里，`session/set_config_option` 的既有校验可以直接切换到它们；不会放宽到任意未声明的 id。
- 标题线程仍是临时 `thread/start`；`CODEX_ACP_TITLE_MODEL` 优先，其次 `ModelId.fromString(currentModelId).model`，最后才是 `TITLE_MODEL` 常量。标题生成失败仍按上游逻辑静默忽略。
- 模型列表里出现 Codex 目录模型是正常的：`model/list` 仍返回 Codex 内置目录，自定义模型只是追加项；provider 是否会实际接受某个内置模型由用户/供应商决定。

## 模型 catalog（DeepSeek）

Codex 内置 catalog 不包含自定义 model id。provider 开启、模型列表非空且目标看起来是 DeepSeek（provider 名称、Base URL 或模型 id 含 `deepseek`）时，应用把 DeepSeek 官方 setup 脚本（`codex-deepseek-setup.sh`）里的 Flash/Pro 元数据写入 `<data>/codex-models.json`，并通过 `CODEX_CONFIG.model_catalog_json` 把绝对路径交给 Codex；否则 Codex 会在 turn 开始时打印 `Model metadata ... not found. Defaulting to fallback metadata`。

- 每个 `models` 条目生成一个 catalog 条目，`slug` 就是设置里的 id（例如 `deepseek/deepseek-v4.1-flash`），不是脚本里的 `deepseek-flash`；含 `pro` 的 id 用 Pro 模板（text only），其余用 Flash 模板（text+image）。`display_name` 同样使用该 id。
- 工具类型、shell、图片输入等来自模板；`context_window`/`max_context_window` 由设置的 `contextWindow` 覆盖（默认 1,048,576），`auto_compact_token_limit` 由 `contextWindow × autoCompactPercent` 计算（默认 90%，即 943,718），`default_reasoning_level` 和 `supported_reasoning_levels` 由 `reasoningEfforts` 覆盖。
- 每个条目强制 `multi_agent_version: "v1"`，覆盖 setup 模板里的 `v2`。Codex Multi-Agent V2 把 spawn 任务作为 OpenAI 私有的 `agent_message` item 发送；Command Code 等第三方 Responses 网关只接受标准 item，会返回 `unsupported_input_item`（文案常为 `Item references need stored responses, which are not kept`），子代理第一条请求就失败。V1 把任务当普通 user message 投递，这些网关可以正常接收。该选择只影响新线程：线程的 multi-agent 版本在首轮确定并被子代理继承，已经按 V2 运行的线程不会因为切换模型而降级，需要新开会话。上游问题见 openai/codex#33551。
- 每个条目同时写入 `base_instructions` 和 `model_messages.instructions_template`，内容都是 setup 脚本里同一份 17 KB Codex 指令 prompt（`internal/codexprovider/catalog_instructions.txt`）。Codex 0.164+ 拒绝缺少这两者的 catalog 条目（`model ... is missing both ...`），不能依赖它自己的默认指令；`base_instructions` 只兼容仍读旧字段的版本，新版本优先 `model_messages.instructions_template`。生成的 catalog 每个模型约 36 KB，只写数据目录，不写进 settings.json。
- 文件在启动加载设置和保存 provider 时原子重写；provider 关闭或模型为空时删除，避免陈旧 metadata。
- 该 catalog 会替换 Codex 内置 catalog，所以开启自定义 provider 时模型列表以这些自定义模型为准；非 DeepSeek 目标或 provider 关闭时不写 catalog，仍只追加 ACP 模型，warning 可能继续。
- 测试：`go test ./internal/codexprovider`（id 替换、可配置 context/compact、档位、指令字段、V1 强制、原子写/删除）、`go test ./internal/ui -run TestCodexProviderWritesModelCatalog`。

## 生效范围

保存只重建 `App.Agents` 中的 Codex launch spec，并清空该 agent 的配置探测缓存与 `model-config.json` 条目；如果启用状态、Base URL 或模型列表变了，还会清掉 `lastSelections.codex` 里的 model/thinking 和草稿的 `PendingConfig`（保留 session mode），避免新 provider 启动时补发旧模型。只改推理档位时保留 model，只清掉旧的 thinking 选择，等新的配置探测给出档位。

- 新建草稿、下次 Connect、配置探测使用新的 provider/模型/上下文/压缩比例/推理档位/标题配置。
- 已连接的 Codex 会话保持启动时的 provider，不重启进程；断开后再次 Connect 才使用新值。
- 只改标题模型、上下文大小或压缩比例不会清掉已选模型；只改推理档位会清掉 `lastSelections.codex` 和草稿里的旧 thinking，保留 model；改 provider 开关、Base URL 或模型列表会清掉 `lastSelections.codex` 的 model/thinking 和草稿 `PendingConfig`（保留 session mode）。
- 设置面板按开关状态提示新会话使用自定义 provider、只使用 model/context/reasoning/title 覆盖，或回到自己的配置。
- 若 Codex 当前不可用（没装 Node/CLI），设置仍会保存；下次 discovery 成功时套用。

## 安全

- 不读取 `~/.codex` 的 `auth.json` / `config.toml`，不代登录。
- API key 仅保存在用户数据目录的 `settings.json`，文件权限 0600；通过子进程环境传递，因此同用户进程可读，这是 Codex 自身配置 provider 的同等信任级别。
- `baseURL` 拒绝 URL 内嵌 `user:password`，避免日志/错误消息泄漏；API key 不进入 `http_headers`、不进入 `-c` 参数。
- 错误日志只记录 provider id、Base URL 和 header 名，不记录 key；codex-acp 的上游日志策略同样不打印 header 值。
- 不自动请求 provider 的 `/models`，避免保存/打开设置时把 key 发给未验证的地址。

## 测试

- `go test ./internal/codexprovider`：URL/模型/上下文/压缩比例/推理档位校验、默认模型与默认档位、环境变量和 `CODEX_CONFIG` 内容、catalog 生成/原子写/删除。
- `go test ./internal/agent`：`CodexLaunchSpec` 在启用/禁用/非法 provider 下的环境。
- `go test ./internal/settings`：`codexProvider` 往返和非法条目的警告 + 自动禁用。
- `go test ./internal/ui -run TestSettingsCodexProvider`：设置分区、上下文/压缩输入、推理档位复选框、保存、持久化和错误提示；`TestCodexProviderWritesModelCatalog` 覆盖 catalog 的 context/compact 字段、落盘和启动重写。
- `codex-acp-main` 内 `CustomModels.test.ts`、`TitleGenerator.test.ts`：自定义模型合并、档位解析/默认档位、标题模型优先级。
- 手工冒烟：用本地 mock Responses 服务填 Base URL 和上下文/压缩比例，勾选几个推理档位，在模型面板确认自定义模型和这些档位出现并切换；确认上下文环使用配置的可用大小，跨过触发点后 Codex 执行压缩；再确认标题生成请求使用 `titleModel` 或当前模型。

## 参考

- [codex.md](codex.md)：bundle 构建、补丁与升级。
- [architecture.md](architecture.md)：设置存储与 ACP 链路。
- Codex provider 配置字段：`model_provider`、`model_context_window`、`model_auto_compact_token_limit`、`model_providers.<id>.{name,base_url,wire_api,env_key,requires_openai_auth}`。
- DeepSeek Codex setup 脚本与 catalog：https://cdn.deepseek.com/api-docs/codex-deepseek-setup.sh
- Multi-Agent V2 与外部 Responses provider 的兼容问题：https://github.com/openai/codex/issues/33551
