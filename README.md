# cc-qq-gateway

A QQ private-chat gateway for running a local **Codex CLI** session from a QQ
Bot. Send a C2C/private message to the bot, the gateway runs `codex exec`, and
the reply is sent back to the same QQ conversation with resumable context.

```
QQ user -> QQ platform -> WebSocket/Webhook -> cc-qq-gateway -> codex exec
   ^                                                            |
   +---------------- passive reply / active fallback -----------+
```

## Features

- **Single-chat QQ surface**: C2C/private messages only. Group, guild, channel,
  DM, button and reaction surfaces are intentionally not implemented.
- **Two transports**: WebSocket for local outbound-only operation, or Webhook
  for public HTTPS callbacks with QQ signature validation.
- **Codex sessions**: one resumable Codex thread per QQ conversation, persisted
  across restarts through `state_path`.
- **Always-online runtime**: transport supervision, panic recovery, heartbeat
  watchdog, token refresh and systemd-friendly process behavior.
- **Rich media I/O**: inbound QQ files are downloaded to `media_dir`; Codex can
  return files/images by emitting `@@QQ_FILE:` / `@@QQ_IMAGE:` directive lines.
- **Long replies**: split into QQ-safe chunks, or uploaded as a `.md` file when
  the reply cannot fit the passive-reply budget.
- **Local notify endpoint**: optional loopback-only `/notify` endpoint for
  trusted local processes to push proactive QQ messages.
- **Clean command surface**: legacy provider-specific commands were removed; `/mcp`,
  `/doctor`, `/resume`, `/compact`, `/think` now target Codex behavior.

## Quick Start

Prerequisites:

- Go 1.23+.
- Codex CLI on `PATH` (`codex --version`) and authenticated for the service user.
- QQ Bot AppID and AppSecret from the QQ Open Platform.

```bash
cp config.example.toml config.toml
make build
./bin/cc-qq-gateway -config config.toml
```

Message the bot from QQ after the transport logs a successful connection.

## Configuration

See [config.example.toml](config.example.toml) for all options. The AI section
uses `[codex]` for the live Codex CLI settings:

| Setting | Meaning |
| --- | --- |
| `codex.binary` | Codex executable, normally `"codex"`. |
| `codex.work_dir` | Codex working directory. |
| `codex.model` | Optional Codex model id. Empty uses Codex config/default. |
| `codex.effort` | Optional reasoning effort: `minimal`, `low`, `medium`, `high`, `xhigh`. |
| `codex.permission_mode` | Gateway mode: `default`, `plan`, `acceptEdits`, `bypassPermissions`. |
| `codex.sandbox` / `codex.approval_policy` | Codex defaults used by `permission_mode = "default"`. |
| `codex.dangerously_skip_permissions` | Runs Codex with `--dangerously-bypass-approvals-and-sandbox`. |
| `codex.web_search` | Enables Codex native web search. |
| `gateway.allowed_users` | Optional C2C open_id allowlist. Use `/whoami` to learn your open_id. |

For full server authority, set `work_dir = "/home/codex"`, `add_dirs = ["/"]`,
`permission_mode = "bypassPermissions"` and
`dangerously_skip_permissions = true`. Anyone allowed to message the bot can then
ask Codex to operate on the host, so keep `gateway.allowed_users` locked down.

## Commands

Anything that is not a recognized gateway command is passed to Codex as the
prompt, including unknown slash-prefixed text.

**Conversation**

| Command | What it does |
| --- | --- |
| `/new` | Start a fresh Codex thread. |
| `/retry` | Re-run the last user message. |
| `/stop` | Cancel the running turn. |
| `/compact [focus]` | Ask Codex for a handoff summary, clear the thread, and seed the next turn with that summary. |

**Session**

| Command | What it does |
| --- | --- |
| `/resume` | List recent Codex threads for the working directory. |
| `/resume <n\|id-prefix>` | Attach this QQ conversation to a listed or matching Codex thread. |
| `/sessions` | Show live QQ conversations tracked by the gateway. |
| `/status` | Show current thread, model, effort, directory, permission mode and live tool activity. |

**Configuration**

| Command | What it does |
| --- | --- |
| `/model [id]` | Show or set the per-conversation Codex model. `default` clears the override. |
| `/effort [level]` | Show or set reasoning effort. `default` clears the override. |
| `/think` | Make the next turn use `xhigh` effort. |
| `/dir [path]` | Show or set the working directory. `default` clears the override. |
| `/mode [name]` | Permission mode: `default`, `plan`, `acceptEdits`, `bypass`. |
| `/timeout [min]` | Show or set the per-turn timeout. `default` clears the override. |

**Codex Management**

| Command | What it does |
| --- | --- |
| `/mcp` | Run `codex mcp list`. |
| `/doctor` | Run `codex doctor` and return the diagnostic output. |

**Shortcuts**

| Command | What it does |
| --- | --- |
| `/review` | Review current code changes. |
| `/diff` | Summarize `git status` and `git diff`. |
| `/explain <x>` | Explain the given code or topic. |
| `/web <q>` | Search the web and answer with sources. |
| `/init` | Create or update project `AGENTS.md`. |

**Info**

| Command | What it does |
| --- | --- |
| `/whoami` | Show your QQ C2C open_id. |
| `/usage` | Show Codex login state, recent token usage, and subscription-window availability. |
| `/version` | Show gateway version and uptime. |
| `/ping` | Liveness check. |
| `/help` | Compact command menu. Use `/help all` for details. |

Removed legacy provider-specific commands: `/agents`, `/memory`, `/cost`, `/export`.

## Run As A Service

```ini
[Unit]
Description=cc-qq-gateway (Codex CLI <-> QQ Bot)
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
User=codex
Group=codex
WorkingDirectory=/home/codex/anything/cc-qq-gateway
Environment=HOME=/home/codex
Environment=CODEX_HOME=/home/codex/.codex
ExecStart=/home/codex/anything/cc-qq-gateway/bin/cc-qq-gateway -config /home/codex/anything/cc-qq-gateway/config.toml
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now cc-qq-gateway
sudo journalctl -u cc-qq-gateway -f
```

## Codex Bridge

New turns run:

```bash
codex [global flags] exec --json --skip-git-repo-check -
```

Resumed turns run:

```bash
codex [global flags] exec resume --json --skip-git-repo-check <thread_id> -
```

The gateway reads JSONL events from stdout, stores the `thread_id`, records tool
activity for progress notices, and sends the final `agent_message` back to QQ.

## Project Layout

```
cmd/cc-qq-gateway/      CLI entrypoint
internal/
  app/                  wiring + transport selection
  config/               TOML config loading and validation
  qq/                   QQ Bot OpenAPI v2 client and WS/Webhook transports
  codex/                Codex CLI bridge
  session/              per-conversation session manager
  gateway/              QQ event -> Codex turn -> QQ reply orchestration
```

## Tests

```bash
make test
make vet
```

## Notes

- QQ passive replies are limited by time window and count; the gateway falls back
  to active push and finally queues replies for the next inbound message.
- Native markdown must be enabled for your QQ bot before `reply_as_markdown`
  should be turned on.
- This project is independent and not affiliated with Tencent/QQ.
