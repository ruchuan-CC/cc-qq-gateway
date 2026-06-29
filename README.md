# cc-qq-gateway

A gateway that connects your **local Claude Code** to a **QQ Bot** for
single-chat (private) conversational dialogue. Inspired by
[`chenhg5/cc-connect`](https://github.com/chenhg5/cc-connect), this is a focused,
single-platform (QQ) implementation written in Go.

Send a message to your QQ bot in a **private (C2C) chat** and the gateway runs a
Claude Code turn on your machine and replies with the result, keeping
conversational context. This gateway is **single-chat only** — group, guild
channel, guild DM and button-interaction surfaces are intentionally not
implemented.

```
QQ user ──▶ QQ platform ──▶ (WebSocket or Webhook) ──▶ cc-qq-gateway ──▶ claude (local) 
   ▲                                                          │
   └──────────────  passive reply  ◀──────────────────────────┘
```

## Features

- **Two transports, selectable in config**
  - **WebSocket** — the gateway dials out to QQ's gateway; **no public IP
    required**. Ideal for running next to Claude Code on a laptop/desktop.
    Handles `Identify`, `Heartbeat`, `Resume`, `Reconnect`/`Invalid Session`
    with automatic backoff and session resume.
  - **Webhook** — QQ pushes events to your HTTPS endpoint. Implements the
    Ed25519 **URL-validation challenge** (op 13) and **signature verification**
    of every push, with the key derived from your bot secret exactly per spec.
- **Conversational sessions** — one resumable Claude Code session for the
  single-chat conversation, with idle-reset and in-order, non-overlapping turns.
- **Single-chat (C2C) surface** — handles the C2C message plus the user/friend
  lifecycle events (`FRIEND_ADD`, `FRIEND_DEL`, `C2C_MSG_RECEIVE`,
  `C2C_MSG_REJECT`), all on the one `GROUP_AND_C2C_EVENT` (`1<<25`) intent. A new
  friend / re-enabled push is greeted via a free `event_id` passive reply.
- **Quota-aware delivery** — per the QQ docs a passive reply is valid for **60
  minutes** (5 per inbound message) while active pushes are capped at **4 per
  month**, so replies are always sent passively first and only fall back to a
  single active push (then the next-message queue) if the window has truly
  expired. Passive `msg_seq` is **per-conversation monotonic** (shared across
  every reply / active push / notify to that user) so a seq is never reused — QQ
  rejects reuse with code `40054005`.
- **Rich built-in commands** — `/help`, `/status`, `/usage`, `/sessions`,
  `/doctor` and more render as **aligned box tables** (QQ-safe: a monospace code
  block, since QQ doesn't render Markdown pipe tables). English + Chinese aliases.
  See [Commands](#commands).
- **Always-online resilience** — a supervised, self-healing connection that
  reconnects forever, detects dead ("zombie") links with a heartbeat watchdog,
  recovers from panics, and resumes the gateway session across drops. See
  [Staying online](#staying-online).
- **Full server authority** — out of the box the bundled `config.toml` runs every
  turn with permissions bypassed and the whole filesystem in scope, so Claude can
  do anything on the host. See [Full authority](#full-authority).
- **Long-reply handling** — replies are split on line boundaries and capped to
  the QQ message-length and 5-passive-reply limits.
- **Focused C2C client** — implements exactly the v2 endpoints single-chat
  dialogue needs (see [API coverage](#api-coverage)); nothing more.

## Quick start

### 1. Prerequisites

- **Go 1.23+** to build.
- **Claude Code** installed and on your `PATH` (`claude --version`).
- A QQ bot registered at the [QQ Open Platform](https://q.qq.com) with its
  **AppID** and **AppSecret**.

### 2. Configure

```bash
cp config.example.toml config.toml
# edit config.toml: set qq.app_id and qq.client_secret
```

(A `config.toml` prefilled with the credentials you provided is already
included — it is gitignored.)

### 3. Build & run

```bash
make build
./bin/cc-qq-gateway -config config.toml
# or simply:
make run
```

You should see the bot authenticate and the transport start. Message your bot
from QQ and it will reply with Claude Code's output.

## Configuration reference

See [`config.example.toml`](config.example.toml) for every option with inline
documentation. Key choices:

| Setting | Meaning |
| --- | --- |
| `qq.transport` | `"websocket"` (no public IP) or `"webhook"` (public HTTPS). |
| `qq.intents` | Event subscription (WebSocket). Single-chat needs only `GROUP_AND_C2C_EVENT`. |
| `claude.work_dir` | The project directory Claude Code operates in. |
| `claude.permission_mode` / `claude.dangerously_skip_permissions` | How tool permissions are handled in non-interactive turns. For autonomous dev tasks set `dangerously_skip_permissions = true`; for chat-only, leave defaults. |
| `gateway.session_idle_minutes` | Auto-reset a conversation after inactivity. |
| `gateway.reply_as_markdown` | Send replies as markdown (needs the capability enabled on your bot). |
| `gateway.allowed_users` | Optional C2C allowlist (user open_ids). Empty = any user. |

### WebSocket vs Webhook

- **WebSocket** is the simplest for local use — nothing inbound to expose. The
  gateway only makes outbound connections.
- **Webhook** requires a public HTTPS URL (QQ allows ports 80/443/8080/8443).
  Register the URL + secret in the console; the gateway answers the validation
  challenge automatically and verifies every push signature. Terminate TLS
  either in-process (`webhook_tls_cert`/`webhook_tls_key`) or at a reverse proxy.

## Commands

The command set is deliberately small — just the controls that matter for an
agentic Claude Code session. Everything else is done by simply telling Claude
what you want. The first token is matched case-insensitively; English and Chinese
aliases both work.

**Conversation & control**
| Command | What it does |
| --- | --- |
| `/new` | Start a fresh conversation (clears context). |
| `/retry` | Re-run your last message. |
| `/stop` | Interrupt the running task. |

**Configuration**
| Command | What it does |
| --- | --- |
| `/model [name]` | Show / switch / `default`-reset the model. |
| `/think` | Make the next reply use extended thinking. |
| `/dir [path]` | Show / change / `default`-reset the working directory. |
| `/mode [name]` | Permission mode: `default` / `plan` / `acceptEdits` / `bypass`. |

**Claude Code management** (run the real CLI feature)
| Command | What it does |
| --- | --- |
| `/agents` | List background agents (`claude agents --json`). |
| `/mcp` | List configured MCP servers (`claude mcp list`). |
| `/memory` | Show the CLAUDE.md memory Claude loads (global + project). |
| `/doctor` | Environment health: Claude version, auth/plan, gateway, authority. |

**Claude Code feature shortcuts** (run a turn with a canned prompt)
| Command | What it does |
| --- | --- |
| `/review` | Review current changes for bugs and improvements. |
| `/diff` | Summarize `git status` + `git diff`. |
| `/explain <x>` | Explain the given code/topic. |
| `/web <q>` | Web search with a sourced answer. |
| `/init` | Create/update CLAUDE.md for the project. |

**Info & usage**
| Command | What it does |
| --- | --- |
| `/usage` | Subscription usage: 5-hour & 7-day windows (utilization + reset time), per-model, plan, token validity. |
| `/cost` | Time and cost of the last reply. |
| `/status` | Session, model, dir, mode, authority, turns, uptime. |
| `/help` | Capabilities + the command table. |

Anything that isn't a recognized command is sent to Claude as a prompt. An
unrecognized `/slash` token is reported instead of being forwarded, so typos
don't silently become prompts.

## Staying online

The gateway is built to connect on startup and never stay down:

- **Supervised transport** — `app.Run` runs the selected transport in a loop with
  panic recovery and jittered backoff; the only way it returns is a clean
  shutdown signal.
- **Forever-reconnecting WebSocket** — exponential backoff (capped at 30s, with
  ±20% jitter) that **resets after a stable connection**, so an established link
  that drops once retries immediately. `Resume`/`Reconnect`/`Invalid Session` are
  all handled, resuming the session and event sequence where possible.
- **Heartbeat watchdog** — any inbound frame marks the link alive; if the gateway
  goes silent past ~2× the heartbeat interval the connection is treated as a
  zombie, force-closed, and re-established. A cancelled context closes the socket
  immediately so blocked reads unblock at once.
- **Resilient token refresh** — the access token auto-refreshes before expiry and
  falls back to the still-valid cached token if a refresh request fails.
- **Startup identity check** — bot identity is confirmed (with retries) at boot so
  a successful connection is observable in the logs.

Run it under a process manager (systemd, supervisor, `pm2`, a container restart
policy, …) for the final layer — if the process itself ever dies it comes right
back, and on restart the WebSocket session resumes.

## Full authority

By design, "the whole server is Claude's home." The bundled `config.toml` sets:

- `dangerously_skip_permissions = true` and `permission_mode = "bypassPermissions"`
  — turns run without permission prompts, so Claude is never blocked mid-task.
- `work_dir = "/home/claude"` and `add_dirs = ["/"]` — start in the home directory
  with the entire filesystem in scope.
- an `append_system_prompt` that tells Claude it has full administrative authority
  over the host.

This is intentionally powerful: anyone who can message the bot can run anything on
the machine. **Lock it down** with `gateway.allowed_users` (use `/whoami` to get
your id), and/or tighten `allowed_tools` / `permission_mode` for a more restricted
deployment.

### Restricted mode

As soon as `allowed_users` is non-empty, the gateway serves **only** those C2C
(single-chat) user open_ids and ignores messages from anyone else. Since this is a
single-chat-only gateway, there are no other surfaces to gate.

## Run as a service (always-on)

To keep the gateway online across crashes and reboots, run it under systemd
(`/etc/systemd/system/cc-qq-gateway.service`):

```ini
[Unit]
Description=cc-qq-gateway (Claude Code <-> QQ Bot)
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
User=claude
WorkingDirectory=/home/claude/anything/cc-qq-gateway
Environment=HOME=/home/claude
ExecStart=/home/claude/anything/cc-qq-gateway/bin/cc-qq-gateway -config config.toml
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now cc-qq-gateway
sudo systemctl status cc-qq-gateway
sudo journalctl -u cc-qq-gateway -f
```

`Restart=always` + the in-process supervisor + `WantedBy=multi-user.target` give
three independent layers of "stay online": process crash → systemd relaunch in
seconds; transport error → in-process reconnect; host reboot → start on boot.

## Full Claude Code over QQ (multimodal I/O)

Talking to the bot is meant to feel like local Claude Code / the Claude app —
every capability is available, with no artificial restrictions:

- **All tools run.** Because each turn invokes the real `claude` CLI with
  permissions bypassed and the whole filesystem in scope, Claude can run shell
  commands, read/write files, search the web, drive MCP servers, spawn subagents,
  etc. Anything you can do in local Claude Code, you can ask for over QQ.
- **Send images & files in.** Attachments on your QQ message are downloaded to
  `media_dir` and their local paths are handed to Claude, which reads them with
  the Read tool — so it can actually *see* images and *read* documents you send.
- **Get media back out.** Claude can return files/images by emitting a directive
  line, which the gateway strips and delivers as QQ rich media:
  ```
  @@QQ_IMAGE: /home/claude/chart.png
  @@QQ_FILE:  /home/claude/report.pdf
  @@QQ_VIDEO: https://example.com/clip.mp4
  @@QQ_AUDIO: /home/claude/voice.silk
  ```
  Local paths are uploaded; `http(s)` URLs are passed through (C2C supports full
  rich-media upload).
- **No truncation.** Replies too long for QQ's 5-message passive-reply budget are
  delivered in full as an uploaded `.md` file (toggle with
  `send_long_replies_as_file`).

All of this is injected into Claude's system prompt automatically, so it knows how
to use the QQ I/O channel on every turn.

## How the Claude Code bridge works

Each turn runs:

```
claude --print --output-format json [--resume <session_id>] [flags] "<prompt>"
```

in `claude.work_dir`. The JSON result yields the reply text and a `session_id`,
which is stored per conversation and passed via `--resume` on the next message,
preserving context. Idle conversations reset automatically.

## Project layout

```
cmd/cc-qq-gateway/      CLI entrypoint
internal/
  app/                  wiring + transport selection (the "daemon")
  config/               TOML config loading & validation
  qq/                   QQ Bot OpenAPI v2 client (single-chat) + WS/Webhook
    types.go            C2C data models
    intents.go          gateway intent (GROUP_AND_C2C_EVENT)
    events.go           event names, opcodes, payload types
    token.go            access-token manager (auto-refresh)
    client.go           authenticated HTTP core
    message.go          C2C send + rich-media upload
    bot.go              GET /users/@me + GET /gateway/bot
    ws.go               WebSocket transport (identify/heartbeat/resume)
    webhook.go          Webhook transport (Ed25519 validate + verify)
  claude/               local Claude Code CLI bridge
  session/              per-conversation session manager
  gateway/              event → Claude → reply orchestration
```

## API coverage

This is a **single-chat (C2C) only** gateway. The `internal/qq` package implements
just the endpoints that private dialogue needs:

- **Auth & gateway**: `getAppAccessToken` (auto-refresh), `GET /gateway/bot`,
  `GET /users/@me`.
- **Messages — send**: C2C (`POST /v2/users/{openid}/messages`).
- **Messages — rich media**: C2C file upload (image/video/audio/file).
- **Message types**: text and native markdown.

The only conversational event handled is `C2C_MESSAGE_CREATE` (plus the WebSocket
lifecycle `READY`/`RESUMED`); every other QQ surface — group, guild channel, guild
DM, button interactions, reactions, audio, audit — was intentionally removed.

## Tests

```bash
make test
```

Covered: Ed25519 webhook key derivation, the op-13 validation round-trip,
push-signature verification (positive + tamper cases), event dispatch, mention
stripping, reply chunking, and the command-alias registry.

## Notes & limits

- Passive replies are valid for a limited window (≈60 min for C2C) and capped at
  5 per inbound message; the gateway respects this when chunking long output.
- Some features are capability-gated by QQ (e.g. native markdown). Enable the
  relevant capabilities in the bot console.
- This project is independent and not affiliated with Tencent/QQ.

## License

MIT
