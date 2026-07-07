# cc-qq-gateway

A local QQ C2C/private-chat bridge for the Codex CLI.

The runtime is intentionally thin:

```text
QQ private text/files -> QQ WebSocket -> cc-qq-gateway -> codex exec/resume --json
QQ private text       <- QQ OpenAPI  <- cc-qq-gateway <- Codex final agent message
```

There is no gateway command layer. If QQ sends `/status`, `/help`, `/model`, or
any other slash-prefixed text, Codex receives that same text as the prompt.

## What It Does

- Keeps a local QQ Bot WebSocket connection online.
- For each QQ private user, keeps one resumable Codex thread id.
- Sends QQ text directly to `codex exec --json` or
  `codex exec resume --json <thread_id>`.
- Downloads inbound QQ attachments and appends their local paths to the Codex
  prompt. If the QQ message contains only attachments, they are saved and held
  for the same user's next text message.
- Sends Codex's final text reply back to the same QQ private chat.
- Splits long QQ replies into safe text chunks; if delivery fails, it tries one
  active push and then queues the remaining text for the user's next message.
- Persists thread ids, QQ `msg_seq`, turn counts and queued text in `state_path`.

## What It Does Not Do

- No gateway slash commands.
- No welcome/typing/thinking/progress notices.
- No webhook server.
- No local notify endpoint.
- No outbound `@@QQ_FILE` / `@@QQ_IMAGE` protocol.
- No per-chat model, directory, permission or timeout overrides.

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
| `gateway.max_reply_chars` | Max runes per QQ text chunk. |
| `gateway.reply_as_markdown` | Try QQ native markdown first, then fall back to text on rejection. |
| `gateway.state_path` | Persisted thread/queue state path; `none` disables persistence. |
| `gateway.attachment_dir` | Local directory for inbound QQ attachments. |
| `gateway.attachment_max_bytes` | Per-attachment download cap; default is 512 MiB. |

For full local authority, configure Codex the same way you would when using the
CLI directly, for example `permission_mode = "bypassPermissions"`,
`dangerously_skip_permissions = true`, and `add_dirs = ["/"]`. Lock
`gateway.allowed_users` down when using full authority.

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
