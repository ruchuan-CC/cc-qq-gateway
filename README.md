# cc-qq-gateway

A local QQ C2C/private-chat bridge for the Codex CLI.

The runtime is intentionally thin, with only five QQ-side control commands:

```text
QQ private text/files -> QQ WebSocket -> cc-qq-gateway -> codex exec/resume --json
QQ private text       <- QQ OpenAPI  <- cc-qq-gateway <- Codex final agent message
```

普通 QQ 消息会直接进入 Codex；只有 `/help`、`/model`、`/permissions`、
`/plan`、`/goal` 这 5 个英文指令由网关处理。其它 `/xxx` 文本仍会原样发给
Codex。要把这 5 个指令当普通 prompt 发送，可以用双斜杠转义，例如
`//model gpt-5.5`。

## What It Does

- Keeps a local QQ Bot WebSocket connection online.
- For each QQ private user, keeps one resumable Codex thread id.
- Sends QQ text directly to `codex exec --json` or
  `codex exec resume --json <thread_id>`.
- Supports five QQ-side commands for help, model, permissions, one-shot plan
  mode and per-user goal context.
- Downloads inbound QQ attachments and appends their local paths to the Codex
  prompt. If the QQ message contains only attachments, they are saved and held
  for the same user's next text message.
- Sends Codex's final text reply back to the same QQ private chat, optionally
  using QQ native markdown with a plain-text fallback.
- Splits long QQ replies into safe text chunks; if delivery fails, it tries one
  active push and then queues the remaining text for the user's next message.
- Persists thread ids, QQ `msg_seq`, turn counts and queued text in `state_path`.

## What It Does Not Do

- No broad gateway command system beyond `/help`, `/model`, `/permissions`,
  `/plan` and `/goal`.
- No welcome/typing/thinking/progress notices.
- No webhook server.
- No local notify endpoint.
- No outbound `@@QQ_FILE` / `@@QQ_IMAGE` protocol.
- No QQ-side directory, timeout, MCP, review or arbitrary config commands.

## Quick Start

Prerequisites:

- Go 1.23+.
- Codex CLI installed and authenticated for the same user running the gateway.
- QQ Bot AppID and AppSecret from the QQ Open Platform.

```bash
cp config.example.toml config.toml
make build
./bin/cc-qq-gateway -config config.toml
```

Fill `config.toml` with your real QQ credentials before running. Do not commit
`config.toml`.

## Configuration

See [config.example.toml](config.example.toml). The key settings are:

| Setting | Meaning |
| --- | --- |
| `qq.app_id` / `qq.client_secret` | QQ Bot credentials. |
| `qq.sandbox` | Use the QQ sandbox OpenAPI base. |
| `qq.intents` | WebSocket intents. Empty defaults to `GROUP_AND_C2C_EVENT`. |
| `codex.binary` | Codex executable, normally `codex`. |
| `codex.work_dir` | Working directory for Codex. Empty uses the process cwd. |
| `codex.model` / `codex.effort` | Optional Codex CLI defaults. |
| `codex.permission_mode` | `default`, `plan`, `acceptEdits`, or `bypassPermissions`. |
| `codex.dangerously_skip_permissions` | Adds `--dangerously-bypass-approvals-and-sandbox`. |
| `codex.web_search` | Adds `--search`. |
| `codex.add_dirs` / `codex.extra_args` | Extra Codex CLI flags. |
| `codex.append_system_prompt` | Optional prompt prefix before the QQ text. Empty means no prefix. |
| `codex.timeout_seconds` | Max runtime for one Codex turn. |
| `gateway.allowed_users` | Optional QQ C2C open_id allowlist. Empty allows any private user. |
| `gateway.admin_users` | QQ open_id list allowed to use `/permissions full-access`. Empty follows `allowed_users` for single-user setups. |
| `gateway.max_reply_chars` | Max runes per QQ text chunk. |
| `gateway.reply_as_markdown` | Try QQ native markdown first, then fall back to text on rejection. |
| `gateway.state_path` | Persisted thread/queue state path; `none` disables persistence. |
| `gateway.attachment_dir` | Local directory for inbound QQ attachments. |
| `gateway.attachment_max_bytes` | Per-attachment download cap; default is 512 MiB. |

For full local authority, configure Codex the same way you would when using the
CLI directly, for example `permission_mode = "bypassPermissions"`,
`dangerously_skip_permissions = true`, and `add_dirs = ["/"]`. Lock
`gateway.allowed_users` down when using full authority.

## QQ Usage

在 QQ 私聊里直接发送普通文字即可和 Codex 对话。图片、文件、视频会先下载到
本地，然后把本地路径追加到这次 prompt 后面。只发附件不发文字时，网关会先保
存附件并提示你补一句说明；同一 QQ 用户的下一条文字会带上这些附件路径一起发
给 Codex。

支持的指令只有 5 个：

- **/help** 显示中文使用说明。
- **/model <model|default>** 切换模型，例如 `/model gpt-5.5`；`default`
  恢复 `config.toml` 默认模型。
- **/permissions <read-only|workspace-write|full-access|default>** 切换权限。
  `full-access` 映射到 Codex 的
  `--dangerously-bypass-approvals-and-sandbox`，只有管理员可用。
- **/plan <需求>** 单次只读规划，不改变后续普通聊天的权限。
- **/goal <目标|show|clear>** 设置、查看或清除当前 QQ 用户的会话目标。设置
  后，后续普通消息会自动带上这个目标上下文交给 Codex。

未知 `/xxx` 会作为普通 prompt 发给 Codex。核心指令需要转义时，在前面多加一
个 `/`，例如 `//model gpt-5.5`。

所有提示文案使用中文；指令名和权限值保持英文，方便和 Codex CLI 的概念对齐。

## QQ Markdown Compatibility

QQ 官方 Markdown 在单聊/C2C 里支持自定义 `markdown.content`，发送时使用
`msg_type = 2`。当前网关按官方支持子集输出：

- 保留标题、粗体、斜体、删除线、链接、有序列表、无序列表、引用和水平线。
- 普通多行文本会用空行分隔，避免 QQ 客户端把单个换行吞掉。
- 普通段落后紧跟列表时，会自动插入空行，保证列表能被 QQ 识别。
- GFM 表格不是 QQ 官方支持格式，会降级成无序列表。
- fenced code block 不是 QQ 官方支持格式，会降级成引用块。
- Markdown 图片只适合公网 URL；本地附件仍以本地路径交给 Codex，不会作为
  QQ 出站图片发送。

如果 `gateway.reply_as_markdown = true`，网关会先发 QQ Markdown；QQ API 拒
绝时自动重试纯文本。

## Codex Invocation

New thread:

```bash
codex [global flags] exec --json --skip-git-repo-check -
```

Existing thread:

```bash
codex [global flags] exec resume --json --skip-git-repo-check <thread_id> -
```

The QQ message text is written to stdin. When a message has attachments, the
gateway downloads them first and appends a plain text section like:

```text
QQ 附件：
- image/png: /Users/example/.cc-qq/attachments/c2c_user/msg/1_photo.png
- file: download failed, url=https://example.invalid/a.pdf, error=status 500
```

The gateway reads Codex JSONL stdout, stores the latest `thread_id`, and forwards
the final agent message text to QQ.

## Run As A Service

Build the binary and point your service manager at:

```bash
/absolute/path/to/bin/cc-qq-gateway -config /absolute/path/to/config.toml
```

The process supervises the QQ WebSocket internally and reconnects after network
or gateway failures.

## Project Layout

```text
cmd/cc-qq-gateway/      CLI entrypoint
internal/app/           app wiring and WebSocket supervision
internal/config/        TOML config loading and validation
internal/qq/            QQ Bot OpenAPI client and WebSocket transport
internal/codex/         Codex CLI bridge
internal/session/       per-user Codex thread and reply queue state
internal/gateway/       QQ event -> Codex turn -> QQ reply orchestration
```

## Tests

```bash
make test
make vet
```
