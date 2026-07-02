// Package gateway wires QQ Bot events to a local Claude Code session and routes
// Claude's replies back to the originating QQ conversation.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chenhg5/cc-qq-gateway/internal/claude"
	"github.com/chenhg5/cc-qq-gateway/internal/config"
	"github.com/chenhg5/cc-qq-gateway/internal/qq"
	"github.com/chenhg5/cc-qq-gateway/internal/session"
)

// Version is the gateway build version, surfaced via /version.
const Version = "0.5.2"

// Gateway is the central orchestrator.
type Gateway struct {
	client   *qq.Client
	bridge   *claude.Bridge
	sessions *session.Manager
	cfg      config.GatewayConfig
	logger   *log.Logger

	allowedUsers map[string]bool

	startedAt time.Time

	usageMu    sync.Mutex
	totalTurns int
	totalCost  float64
}

// addUsage accumulates process-wide usage for the /usage command.
func (g *Gateway) addUsage(costUSD float64) {
	g.usageMu.Lock()
	g.totalTurns++
	g.totalCost += costUSD
	g.usageMu.Unlock()
}

func (g *Gateway) usageSnapshot() (int, float64) {
	g.usageMu.Lock()
	defer g.usageMu.Unlock()
	return g.totalTurns, g.totalCost
}

// New builds a Gateway.
func New(client *qq.Client, bridge *claude.Bridge, sessions *session.Manager, cfg config.GatewayConfig, logger *log.Logger) *Gateway {
	if logger == nil {
		logger = log.Default()
	}
	g := &Gateway{
		client:       client,
		bridge:       bridge,
		sessions:     sessions,
		cfg:          cfg,
		logger:       logger,
		allowedUsers: toSet(cfg.AllowedUsers),
		startedAt:    time.Now(),
	}
	return g
}

func toSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}

// HandleEvent is the qq.EventHandler. This gateway is single-chat only: it
// handles the C2C (private) message event and ignores everything else.
func (g *Gateway) HandleEvent(ctx context.Context, p *qq.Payload) {
	switch p.Type {
	case qq.EventC2CMessageCreate:
		var m qq.C2CMessage
		if err := json.Unmarshal(p.Data, &m); err != nil {
			g.logger.Printf("[gateway] decode c2c message: %v", err)
			return
		}
		if g.allowedUsers != nil && !g.allowedUsers[m.Author.UserOpenID] {
			g.logger.Printf("[gateway] ignoring c2c message from non-allowlisted user %s", m.Author.UserOpenID)
			return
		}
		sess := g.sessions.Get("c2c:" + m.Author.UserOpenID)
		r := &responder{
			client:     g.client,
			userOpenID: m.Author.UserOpenID,
			msgID:      m.ID,
			asMarkdown: g.cfg.ReplyAsMarkdown,
			nextSeq:    sess.NextSeq,
		}
		g.dispatch(ctx, r, cleanContent(m.Content), m.Attachments)

	case qq.EventFriendAdd, qq.EventC2CMsgReceive:
		// User added the bot / re-enabled message push. Greet them — using the
		// event_id so the welcome is a free passive reply (no scarce active-push
		// quota). event_id is only carried over the webhook transport (p.ID); over
		// WebSocket there is none, so we just log and skip rather than spend quota.
		g.handleFriendEvent(ctx, p, true)
	case qq.EventFriendDel, qq.EventC2CMsgReject:
		// User removed the bot / turned off push. Nothing to send (and we mustn't);
		// just record it.
		g.handleFriendEvent(ctx, p, false)
	case qq.EventReady, qq.EventResumed:
		// WebSocket lifecycle — handled by the transport (session tracking); nothing to do here.
	default:
		// Single-chat only: every other QQ surface/event is ignored.
		g.logger.Printf("[gateway] ignoring non-C2C event %s", p.Type)
	}
}

// welcomeText greets a user who just added the bot / re-enabled push.
const welcomeText = "**👋 你好，我是 Claude Code。**\n直接把需求发给我即可——写代码、查资料、读图片/文件都行。\n发送 **/help** 查看全部命令。"

// handleFriendEvent records a single-chat user/friend lifecycle event and, when
// greet is set and the transport provided an event_id (webhook only), sends a
// free passive welcome. It never spends active-push quota and respects the
// allow-list.
func (g *Gateway) handleFriendEvent(ctx context.Context, p *qq.Payload, greet bool) {
	var ev qq.C2CManageEvent
	if err := json.Unmarshal(p.Data, &ev); err != nil {
		g.logger.Printf("[gateway] decode %s event: %v", p.Type, err)
		return
	}
	openID := ev.User()
	g.logger.Printf("[gateway] %s — user open_id=%s", p.Type, openID)
	if !greet || openID == "" {
		return
	}
	if g.allowedUsers != nil && !g.allowedUsers[openID] {
		return
	}
	if p.ID == "" {
		// No event_id (WebSocket transport): a greeting would require an active push,
		// which is capped at 4/month — not worth spending on a welcome. Skip silently.
		return
	}
	sess := g.sessions.Get("c2c:" + openID)
	r := &responder{
		client:     g.client,
		userOpenID: openID,
		eventID:    p.ID,
		asMarkdown: g.cfg.ReplyAsMarkdown,
		nextSeq:    sess.NextSeq,
	}
	if err := r.Send(ctx, welcomeText); err != nil {
		g.logger.Printf("[gateway] welcome send failed for %s: %v", openID, err)
	}
}

// dispatch handles slash-commands inline and otherwise runs a Claude turn in a
// goroutine so the event loop is never blocked.
func (g *Gateway) dispatch(ctx context.Context, r *responder, text string, atts []qq.MessageAttachment) {
	text = strings.TrimSpace(text)
	if text == "" && len(atts) == 0 {
		return
	}
	key := r.conversationKey()
	g.logger.Printf("[gateway] inbound %s — %s (attachments=%d)", key, r.identity(), len(atts))

	// Surface an idle-TTL context reset that would otherwise be silent: the user
	// continuing an hours-old thread deserves to know the old context is gone
	// rather than getting confusingly amnesiac answers.
	if g.sessions.Get(key).TakeIdleReset() {
		_ = r.Send(ctx, fmt.Sprintf("🕰️ 距上次对话已超过 %d 分钟，已自动开启新上下文（旧话题请重新带上背景）。", g.cfg.SessionIdleMinutes))
	}

	// This inbound message opens a fresh passive-reply window, so flush any replies
	// that a long-running turn couldn't deliver earlier (the next-message fallback
	// that guarantees a result is never lost, even without active-push permission).
	g.flushPending(ctx, r, key)

	if handled := g.handleCommand(ctx, r, key, text); handled {
		return
	}

	// Detach from the request context so a webhook response (which cancels its
	// context on return) doesn't kill the in-flight turn.
	go g.runTurn(context.Background(), r, key, text, atts)
}

// safeGo runs fn in a goroutine with a panic recover, so a panic in one command or
// turn is logged and contained instead of crashing the whole gateway process (the
// transport supervisor cannot recover a panic in a detached goroutine). label names
// the work for the log line.
func (g *Gateway) safeGo(label string, fn func()) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				g.logger.Printf("[gateway] PANIC in %s: %v\n%s", label, rec, debug.Stack())
			}
		}()
		fn()
	}()
}

// flushPending delivers any replies queued from an earlier turn that outran the
// passive-reply window. Delivered as passive replies to the current message, which
// has a fresh window. Sent best-effort; a delivery failure re-queues nothing (the
// next message will retry whatever this call leaves behind via re-queue below).
func (g *Gateway) flushPending(ctx context.Context, r *responder, key string) {
	pending := g.sessions.Get(key).TakePending()
	if len(pending) == 0 {
		return
	}
	defer g.persist()
	for _, p := range pending {
		if err := g.deliver(ctx, r, key, "⏮️ 稍早那条任务的结果：\n\n"+p); err != nil {
			g.logger.Printf("[gateway] [%s] re-queueing pending reply (flush failed: %v)", key, err)
			g.sessions.Get(key).QueuePending(p)
			return
		}
	}
}

// handleCommand processes built-in control commands. A command is any message
// whose first token starts with "/" (or a recognized Chinese alias). Returns
// true if the message was handled as a command (and thus must not reach Claude).
func (g *Gateway) handleCommand(ctx context.Context, r *responder, key, text string) bool {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	name := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))

	canon, ok := commandAliases[name]
	if !ok {
		// Not a built-in gateway command: let the message reach Claude unchanged.
		// This deliberately includes text that merely starts with "/" — a file path
		// ("/etc/hosts 看看这个"), a pasted code snippet, or one of Claude Code's own
		// slash commands — so the gateway behaves like using Claude Code directly
		// instead of swallowing such input as an "unknown command".
		return false
	}

	// Feature shortcuts run a Claude turn with a canned prompt.
	if tmpl, ok := promptShortcuts[canon]; ok {
		g.runShortcut(ctx, r, key, canon, tmpl, arg)
		return true
	}

	switch canon {
	case "new":
		// Cancel any in-flight turn first; otherwise it would finish and write its
		// session id back, silently resurrecting the context we're trying to clear.
		g.sessions.Get(key).CancelTurn()
		g.sessions.Reset(key)
		g.persist()
		_ = r.Send(ctx, "✅ **已开启新对话**，上下文已清空。")
	case "model":
		g.cmdModel(ctx, r, key, arg)
	case "dir":
		g.cmdCwd(ctx, r, key, arg)
	case "mode":
		g.cmdMode(ctx, r, key, arg)
	case "usage":
		g.safeGo("usage", func() { _ = r.Send(context.Background(), g.usageText()) })
	case "mcp":
		g.safeGo("mcp", func() { g.runManaged(r, key, "🔌 MCP 服务器", "mcp", "list") })
	case "agents":
		g.safeGo("agents", func() { g.runManaged(r, key, "🤖 子代理 (agents)", "agents", "--json") })
	case "memory":
		g.safeGo("memory", func() { g.cmdMemory(r, key) })
	case "doctor":
		g.safeGo("doctor", func() { g.cmdDoctor(r, key) })
	case "think":
		g.sessions.Get(key).SetThinkNext()
		_ = r.Send(ctx, "🧠 **下一条回复将进行深度思考。**")
	case "timeout":
		g.cmdTimeout(ctx, r, key, arg)
	case "compact":
		g.safeGo("compact", func() { g.cmdCompact(r, key, arg) })
	case "export":
		g.safeGo("export", func() { g.cmdExport(r, key) })
	case "resume":
		g.safeGo("resume", func() { g.cmdResume(r, key, arg) })
	case "retry":
		last := g.sessions.Get(key).LastPrompt()
		if last == "" {
			_ = r.Send(ctx, "ℹ️ 没有可重发的消息。")
		} else {
			_ = r.Send(ctx, "🔁 **重新执行上一条…**")
			go g.runTurn(context.Background(), r, key, last, nil)
		}
	case "cost":
		cost, dur := g.sessions.Get(key).LastStats()
		if cost == 0 && dur == 0 {
			_ = r.Send(ctx, "ℹ️ 还没有可统计的回复。")
		} else {
			_ = r.Send(ctx, fmt.Sprintf("**💰 上次回复** 用时 %.1fs · 花费 $%.4f", float64(dur)/1000, cost))
		}
	case "stop":
		if g.sessions.Get(key).CancelTurn() {
			_ = r.Send(ctx, "🛑 **正在中断当前任务。**")
		} else {
			_ = r.Send(ctx, "ℹ️ 当前没有正在运行的任务。")
		}
	case "status":
		_ = r.Send(ctx, g.statusText(key))
	case "whoami":
		_ = r.Send(ctx, "## 🪪 你的身份\n\n"+kvLines([][2]string{
			{"类型", "私聊 (C2C)"},
			{"open_id", r.userOpenID},
		})+"\n\n把 open_id 填入配置 allowed_users 即可锁定操作者。")
	case "version":
		_ = r.Send(ctx, fmt.Sprintf("**🏷️ 版本** cc-qq-gateway v%s · 运行 %s", Version, g.uptime()))
	case "ping":
		_ = r.Send(ctx, "🏓 pong · 运行 "+g.uptime())
	case "sessions":
		_ = r.Send(ctx, g.sessionsText())
	case "help":
		g.sendHelp(ctx, r, arg)
	default:
		return false
	}
	return true
}

// modelFullNames are the switchable models on this account, listed by FULL id (no
// short aliases). Fable 5 unlocked on this account and verified accepted by the
// CLI 2026-07-02. Switch with `/model <full name>`.
var modelFullNames = []string{
	"claude-fable-5",
	"claude-opus-4-8",
	"claude-opus-4-8[1m]",
	"claude-sonnet-5",
	"claude-haiku-4-5",
}

// modelListLines renders modelFullNames as a bullet list for QQ markdown.
func modelListLines() string {
	var b strings.Builder
	for _, m := range modelFullNames {
		b.WriteString("- ")
		b.WriteString(m)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// modelHint lists the switchable model full names, shown when a name is unknown.
const modelHint = "可切换模型（用全名）：claude-fable-5 / claude-opus-4-8 / claude-opus-4-8[1m] / " +
	"claude-sonnet-5 / claude-haiku-4-5。" +
	"直接 /model <全名> 即可切换，如 /model claude-fable-5。恢复默认：/model default"

// cmdModel shows or sets the per-conversation model override. The argument is
// normalized to a value the CLI's --model accepts (display names like
// "Opus 4.8 (1M context)" are translated, not passed through), and an
// unrecognized name is rejected instead of being stored and wedging every turn.
func (g *Gateway) cmdModel(ctx context.Context, r *responder, key, arg string) {
	sess := g.sessions.Get(key)
	if strings.TrimSpace(arg) == "" {
		cur := sess.GetModel()
		if cur == "" {
			cur = g.bridge.DefaultModel()
			if cur == "" {
				cur = "CLI 默认"
			}
			cur += "（默认）"
		}
		_ = r.Send(ctx, "## 🧠 模型\n\n"+kvLines([][2]string{
			{"当前", cur},
		})+"\n\n**可切换模型（用全名）**\n"+modelListLines()+
			"\n\n直接 /model <全名> 即可切换，如 **/model claude-fable-5**　恢复默认：/model default")
		return
	}
	canon, ok := claude.NormalizeModel(arg)
	if !ok {
		_ = r.Send(ctx, "⚠️ 无法识别的模型名 "+arg+"。\n\n"+modelHint)
		return
	}
	sess.SetModel(canon)
	g.persist()
	if canon == "" {
		_ = r.Send(ctx, "**🧠 模型** 已恢复默认。")
		return
	}
	_ = r.Send(ctx, "🧠 模型已切换为 **"+canon+"**")
}

// cmdCwd shows or sets the per-conversation working directory override.
func (g *Gateway) cmdCwd(ctx context.Context, r *responder, key, arg string) {
	sess := g.sessions.Get(key)
	if arg == "" {
		cur := sess.GetWorkDir()
		if cur == "" {
			cur = g.bridge.DefaultWorkDir()
			if cur == "" {
				cur = "Claude 默认"
			}
			cur += "（默认）"
		}
		_ = r.Send(ctx, "## 📁 工作目录\n\n"+kvLines([][2]string{
			{"当前", cur},
		})+"\n\n切换：/dir <路径>　恢复默认：/dir default")
		return
	}
	if strings.EqualFold(arg, "default") || strings.EqualFold(arg, "reset") {
		sess.SetWorkDir("")
		g.persist()
		_ = r.Send(ctx, "**📁 工作目录** 已恢复默认。")
		return
	}
	sess.SetWorkDir(arg)
	g.persist()
	_ = r.Send(ctx, "📁 工作目录已切换为 **"+arg+"**")
}

// cmdMode shows or sets the per-conversation permission mode.
func (g *Gateway) cmdMode(ctx context.Context, r *responder, key, arg string) {
	sess := g.sessions.Get(key)
	if arg == "" {
		cur := sess.GetMode()
		if cur == "" {
			cur = "default"
		}
		modes := [][2]string{
			{"default", "按网关配置"},
			{"plan", "只读规划"},
			{"acceptEdits", "自动接受改动"},
			{"bypass", "完全放行"},
		}
		for i := range modes {
			if modes[i][0] == cur {
				modes[i][1] += "（当前）"
			}
		}
		_ = r.Send(ctx, "## 🔐 权限模式\n\n"+kvLines(modes)+"\n\n切换：/mode <名称>")
		return
	}
	norm := arg
	switch strings.ToLower(arg) {
	case "default", "reset":
		sess.SetMode("")
		_ = r.Send(ctx, "**🔐 权限模式** 已恢复默认。")
		return
	case "plan":
		norm = "plan"
	case "acceptedits":
		norm = "acceptEdits"
	case "bypass", "bypasspermissions":
		norm = "bypass"
	default:
		_ = r.Send(ctx, "❓ 未知模式 "+arg+"，可选：default / plan / acceptEdits / bypass")
		return
	}
	sess.SetMode(norm)
	g.persist()
	_ = r.Send(ctx, "🔐 权限模式已切换为 **"+norm+"**")
}

// cmdTimeout shows or sets the per-conversation turn-timeout override.
func (g *Gateway) cmdTimeout(ctx context.Context, r *responder, key, arg string) {
	sess := g.sessions.Get(key)
	if strings.TrimSpace(arg) == "" {
		cur := sess.GetTimeoutMin()
		val := fmt.Sprintf("%d 分钟", cur)
		if cur <= 0 {
			val = fmt.Sprintf("%.0f 分钟（默认）", g.bridge.DefaultTimeout().Minutes())
		}
		_ = r.Send(ctx, "## ⏱️ 单轮任务时限\n\n"+kvLines([][2]string{
			{"当前", val},
		})+"\n\n设置：/timeout <分钟数>（如 /timeout 60）　恢复默认：/timeout default")
		return
	}
	if strings.EqualFold(arg, "default") || strings.EqualFold(arg, "reset") || arg == "默认" {
		sess.SetTimeoutMin(0)
		g.persist()
		_ = r.Send(ctx, "**⏱️ 时限** 已恢复默认。")
		return
	}
	min, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(arg), "m"))
	if err != nil || min < 1 || min > 24*60 {
		_ = r.Send(ctx, "⚠️ 请给一个 1–1440 的分钟数，如 /timeout 60。恢复默认：/timeout default")
		return
	}
	sess.SetTimeoutMin(min)
	g.persist()
	_ = r.Send(ctx, fmt.Sprintf("⏱️ 单轮任务时限已设为 **%d 分钟**", min))
}

// promptShortcuts map a command to a canned prompt that invokes a Claude Code
// capability. Commands in argShortcuts require an argument.
var promptShortcuts = map[string]string{
	"review":  "Review the current code changes (git diff against HEAD) for bugs, risks, and improvements. Be concise and concrete.",
	"diff":    "Run `git status` and `git diff` in the working directory and give me a concise summary of the current changes.",
	"init":    "Create or update a CLAUDE.md in the working directory that documents this project for future Claude Code sessions.",
	"explain": "Explain the following clearly and concisely:",
	"web":     "Search the web and give a concise, sourced answer for:",
}

// argShortcuts require a trailing argument.
var argShortcuts = map[string]bool{"explain": true, "web": true}

// runShortcut launches a Claude turn from a feature-shortcut command.
func (g *Gateway) runShortcut(ctx context.Context, r *responder, key, name, tmpl, arg string) {
	if argShortcuts[name] && arg == "" {
		_ = r.Send(ctx, "用法：/"+name+" <内容>")
		return
	}
	prompt := tmpl
	if arg != "" {
		prompt = tmpl + " " + arg
	}
	go g.runTurn(context.Background(), r, key, prompt, nil)
}

// displayZone is the timezone for user-facing timestamps. The operator's
// standing rule (2026-07-02): always show SERVER time (the server runs UTC), no
// conversion to Beijing time.
var displayZone = time.Local

// runManaged runs a claude management subcommand and delivers its output. It
// escalates like a turn reply (active push, then queue) so slow output is never
// silently lost.
func (g *Gateway) runManaged(r *responder, key, label string, args ...string) {
	out, err := g.bridge.RunCLI(context.Background(), args...)
	if out == "" {
		if err != nil {
			out = "执行失败：" + short(err.Error())
		} else {
			out = "（无输出）"
		}
	}
	g.deliverOrQueue(context.Background(), r, g.sessions.Get(key), key, "**"+label+"**\n"+out)
}

// cmdMemory shows the CLAUDE.md memory files Claude loads (global + project).
func (g *Gateway) cmdMemory(r *responder, key string) {
	home, _ := os.UserHomeDir()
	candidates := []string{filepath.Join(home, ".claude", "CLAUDE.md")}
	if wd := g.bridge.DefaultWorkDir(); wd != "" {
		candidates = append(candidates, filepath.Join(wd, "CLAUDE.md"))
	}
	var b strings.Builder
	b.WriteString("**🧠 记忆 (CLAUDE.md)**")
	found := false
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		found = true
		b.WriteString("\n\n**" + p + "**\n" + strings.TrimSpace(string(data)))
	}
	if !found {
		b.WriteString("\n（暂无 CLAUDE.md 记忆文件；让我“记住…”即可创建）")
	}
	g.deliverOrQueue(context.Background(), r, g.sessions.Get(key), key, b.String())
}

// cmdDoctor reports a practical environment health summary (claude doctor itself
// is interactive-only, so this gives the equivalent useful checks).
func (g *Gateway) cmdDoctor(r *responder, key string) {
	ver, _ := g.bridge.RunCLI(context.Background(), "--version")
	plan := "未知"
	auth := "❌ 失败"
	if u, err := claude.FetchUsage(context.Background()); err == nil {
		auth = "✅ 正常"
		if u.Plan != "" {
			plan = prettyPlan(u.Plan)
		}
	}
	_ = r.Send(context.Background(), "## 🩺 环境诊断\n\n"+kvLines([][2]string{
		{"Claude CLI", strings.TrimSpace(ver)},
		{"订阅认证", auth + " · " + plan},
		{"网关", "v" + Version + " · 运行 " + g.uptime()},
		{"工具权限", authorityLabel(g.bridge.FullAuthority())},
	}))
}

// persist saves all session state to disk (best-effort; failures are logged).
// Called after any durable state change so a gateway restart loses nothing.
func (g *Gateway) persist() {
	if err := g.sessions.SaveState(); err != nil {
		g.logger.Printf("[gateway] persist session state: %v", err)
	}
}

// SaveState exposes session persistence for the app's shutdown/periodic hooks.
func (g *Gateway) SaveState() { g.persist() }

// compactPrompt asks Claude to produce the handoff summary /compact seeds the
// fresh context with. The summary must be self-contained: the next turn starts
// a brand-new session whose only link to the past is this text.
const compactPrompt = `请把我们本次对话到目前为止的全部内容，压缩成一份详尽的中文交接摘要，供“新的你”无缝接手。必须包含：正在进行的任务及其当前进度、已经做过的关键操作和结论、重要的文件路径/命令/数据、我的偏好和约定、尚未完成的下一步。直接输出摘要正文，不要开场白和客套。`

// cmdCompact compacts the conversation: it asks Claude (inside the current
// session) for a handoff summary, then resets the Claude session and stores the
// summary as a seed that is prepended to the next turn — the same effect as
// Claude Code's /compact, reimplemented for print-mode turns.
func (g *Gateway) cmdCompact(r *responder, key, focus string) {
	ctx := context.Background()
	sess := g.sessions.Get(key)
	if sess.GetSessionID() == "" {
		_ = r.Send(ctx, "ℹ️ 当前没有可压缩的上下文（新会话）。")
		return
	}
	if sess.Running() {
		_ = r.Send(ctx, "⏳ 有任务正在运行，等它结束后再 /compact。")
		return
	}
	_ = r.Send(ctx, "🗜️ **正在压缩上下文……** 稍等，压缩完成后对话会以摘要继续。")

	sess.Lock()
	defer sess.Unlock()
	turnCtx, cancel := context.WithCancel(ctx)
	sess.BeginTurn(cancel)
	defer func() {
		sess.EndTurn()
		cancel()
	}()

	prompt := compactPrompt
	if strings.TrimSpace(focus) != "" {
		prompt += "\n\n压缩时请特别保留与此相关的细节：" + focus
	}
	res, err := g.bridge.Run(turnCtx, claude.Request{
		SessionID:      sess.GetSessionID(),
		Prompt:         prompt,
		Model:          sess.GetModel(),
		WorkDir:        sess.GetWorkDir(),
		PermissionMode: sess.GetMode(),
		Timeout:        10 * time.Minute,
	})
	if err != nil || res == nil || res.IsError || strings.TrimSpace(res.Text) == "" {
		detail := "无输出"
		if err != nil {
			detail = short(err.Error())
		} else if res != nil && res.Text != "" {
			detail = short(res.Text)
		}
		g.deliverOrQueue(ctx, r, sess, key, "⚠️ 压缩失败，原上下文保持不变："+detail)
		return
	}
	summary := strings.TrimSpace(res.Text)
	sess.ClearClaude()
	sess.SetSeed(summary)
	g.persist()
	g.deliverOrQueue(ctx, r, sess, key,
		fmt.Sprintf("✅ **上下文已压缩**（摘要 %d 字）。继续对话即可，我会带着摘要接着干；想丢弃摘要重新开始发 /new。", runeLen(summary)))
}

// cmdExport renders the current conversation's CLI transcript as Markdown and
// delivers it as a file.
func (g *Gateway) cmdExport(r *responder, key string) {
	ctx := context.Background()
	sess := g.sessions.Get(key)
	sid := sess.GetSessionID()
	if sid == "" {
		_ = r.Send(ctx, "ℹ️ 当前是新会话，还没有可导出的内容。也可以先 /resume 连上历史会话再导出。")
		return
	}
	workDir := sess.GetWorkDir()
	if workDir == "" {
		workDir = g.bridge.DefaultWorkDir()
	}
	path := filepath.Join(projectDir(workDir), sid+".jsonl")
	md, count, err := renderTranscript(path, sid)
	if err != nil {
		_ = r.Send(ctx, "⚠️ 导出失败："+short(err.Error()))
		return
	}
	dir := filepath.Join(g.cfg.MediaDir, sanitize(key))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_ = r.Send(ctx, "⚠️ 导出失败："+short(err.Error()))
		return
	}
	out := filepath.Join(dir, time.Now().Format("export-0102-150405.md"))
	if err := os.WriteFile(out, []byte(md), 0o600); err != nil {
		_ = r.Send(ctx, "⚠️ 导出失败："+short(err.Error()))
		return
	}
	g.deliverOrQueue(ctx, r, sess, key,
		fmt.Sprintf("📤 **对话已导出**（%d 条消息）\n\n@@QQ_FILE: %s", count, out))
}

// cmdResume lists recent Claude sessions (no argument) or re-attaches the
// conversation to one picked by list number or id prefix — the same recovery
// Claude Code's --resume gives in the terminal.
func (g *Gateway) cmdResume(r *responder, key, arg string) {
	ctx := context.Background()
	sess := g.sessions.Get(key)
	workDir := sess.GetWorkDir()
	if workDir == "" {
		workDir = g.bridge.DefaultWorkDir()
	}
	arg = strings.TrimSpace(arg)

	if arg == "" {
		infos, err := listSessions(workDir, 8)
		if err != nil || len(infos) == 0 {
			_ = r.Send(ctx, "ℹ️ 该工作目录下没有历史会话。")
			return
		}
		cur := sess.GetSessionID()
		ids := make([]string, 0, len(infos))
		rows := make([][2]string, 0, len(infos))
		for i, in := range infos {
			ids = append(ids, in.ID)
			mark := ""
			if in.ID == cur {
				mark = "（当前）"
			}
			rows = append(rows, [2]string{
				fmt.Sprintf("%d.", i+1),
				fmt.Sprintf("%s%s · %s · %s", truncateRunes(in.Title, 24), mark,
					humanDur(time.Since(in.Modified))+"前", in.ID[:8]),
			})
		}
		sess.SetResumeChoices(ids)
		_ = r.Send(ctx, "## ⏪ 历史会话\n\n"+kvLines(rows)+"\n\n用 **/resume <序号>**（或 id 前缀）切换，如 /resume 2。")
		return
	}

	var target string
	if n, err := strconv.Atoi(arg); err == nil {
		target = sess.ResumeChoice(n)
		if target == "" {
			_ = r.Send(ctx, "⚠️ 序号无效。先发 /resume 看列表，再 /resume <序号>。")
			return
		}
	} else {
		target = findSessionByPrefix(workDir, arg)
		if target == "" {
			_ = r.Send(ctx, "⚠️ 找不到（或匹配到多个）该 id 前缀的会话。先发 /resume 看列表。")
			return
		}
	}
	sess.CancelTurn()
	sess.AttachSession(target)
	g.persist()
	title := sessionTitle(filepath.Join(projectDir(workDir), target+".jsonl"))
	if title == "" {
		title = target[:8]
	}
	_ = r.Send(ctx, fmt.Sprintf("⏪ **已切换到历史会话** %s（%s）。直接继续聊即可。", truncateRunes(title, 30), target[:8]))
}

func authorityLabel(full bool) string {
	if full {
		return "完全（无需确认）"
	}
	return "受限"
}

func (g *Gateway) usageText() string {
	u, err := claude.FetchUsage(context.Background())
	if err != nil {
		turns, cost := g.usageSnapshot()
		return fmt.Sprintf("**📊 用量**\n订阅用量获取失败：%s\n本网关累计 %d 轮 · $%.4f", short(err.Error()), turns, cost)
	}
	rows := make([][2]string, 0, 4)
	addWindow := func(label string, w claude.Window) {
		if !w.Has {
			return
		}
		val := fmt.Sprintf("%.0f%%", w.Utilization)
		if !w.ResetsAt.IsZero() {
			val += fmt.Sprintf(" · %s后重置（%s）", humanDur(time.Until(w.ResetsAt)), w.ResetsAt.In(displayZone).Format("01-02 15:04"))
		}
		rows = append(rows, [2]string{label, val})
	}
	addWindow("5 小时", u.FiveHour)
	addWindow("7 天", u.SevenDay)
	addWindow("Opus·7天", u.Opus)
	addWindow("Sonnet·7天", u.Sonnet)

	head := "## 📊 订阅用量"
	if u.Plan != "" {
		head += " · " + prettyPlan(u.Plan)
	}
	return head + "\n\n" + kvLines(rows)
}

func humanDur(d time.Duration) string {
	if d <= 0 {
		return "即将"
	}
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h >= 24:
		return fmt.Sprintf("%d天%dh", h/24, h%24)
	case h > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func prettyPlan(tier string) string {
	switch tier {
	case "default_claude_max_20x":
		return "Claude Max 20x"
	case "default_claude_max_5x":
		return "Claude Max 5x"
	case "default_claude_pro":
		return "Claude Pro"
	}
	return tier
}

func (g *Gateway) statusText(key string) string {
	s := g.sessions.Get(key)
	sid := s.GetSessionID()
	status := "新会话"
	if sid != "" {
		status = "已连接 · " + short(sid)
	}
	model := s.GetModel()
	if model == "" {
		model = g.bridge.DefaultModel()
		if model == "" {
			model = "默认"
		}
	}
	workDir := s.GetWorkDir()
	if workDir == "" {
		workDir = g.bridge.DefaultWorkDir()
		if workDir == "" {
			workDir = "默认"
		}
	}
	authority := "受限"
	if g.bridge.FullAuthority() {
		authority = "完全（无需确认）"
	}
	running := "空闲"
	if d := s.RunningFor(); d > 0 {
		running = "运行中 · 已跑 " + d.Round(time.Second).String()
		if tool, calls, _ := s.ToolActivity(); calls > 0 {
			running += fmt.Sprintf(" · %d 次工具 · 正在用 %s", calls, tool)
		}
	} else if s.Running() {
		running = "运行中"
	}
	timeout := fmt.Sprintf("%.0f 分钟", g.bridge.DefaultTimeout().Minutes())
	if min := s.GetTimeoutMin(); min > 0 {
		timeout = fmt.Sprintf("%d 分钟（本会话）", min)
	}
	rows := [][2]string{
		{"会话", status},
		{"模型", model},
		{"目录", workDir},
		{"权限", authority},
		{"任务", running},
		{"时限", timeout},
		{"轮数", fmt.Sprintf("%d", s.TurnCount())},
		{"运行", g.uptime() + " · v" + Version},
	}
	if s.PeekSeed() != "" {
		rows = append(rows, [2]string{"待续", "有一份压缩摘要将随下一条消息生效"})
	}
	return "## 📊 运行状态\n\n" + kvLines(rows)
}

func (g *Gateway) uptime() string {
	return time.Since(g.startedAt).Round(time.Second).String()
}

// sessionsText summarizes all live conversations, for /sessions.
func (g *Gateway) sessionsText() string {
	snaps := g.sessions.Snapshot()
	if len(snaps) == 0 {
		return "**💬 会话** 暂无活跃会话。"
	}
	turns, cost := g.usageSnapshot()
	rows := make([][2]string, 0, len(snaps))
	for _, s := range snaps {
		state := "空闲"
		if s.Running {
			state = "运行中"
		}
		conn := "新会话"
		if s.Active {
			conn = "已连接"
		}
		rows = append(rows, [2]string{
			short(s.Key),
			fmt.Sprintf("%s · %s · %d 轮 · 闲置 %s", conn, state, s.Turns, time.Since(s.LastActive).Round(time.Second)),
		})
	}
	head := fmt.Sprintf("## 💬 活跃会话 %d 个 · 累计 %d 轮 · $%.4f", len(snaps), turns, cost)
	return head + "\n\n" + kvLines(rows)
}

// commandAliases maps every accepted command token (English + Chinese) to its
// canonical handler. The set is deliberately small — just the controls that
// matter for an agentic Claude Code session; everything else is done by simply
// telling Claude what you want.
var commandAliases = map[string]string{
	// conversation & control
	"/new": "new", "/reset": "new", "/clear": "new", "新对话": "new", "重置": "new", "清空": "new",
	"/retry": "retry", "/redo": "retry", "重试": "retry", "重发": "retry",
	"/stop": "stop", "/cancel": "stop", "/abort": "stop", "停止": "stop", "中断": "stop",
	"/compact": "compact", "压缩": "compact", "压缩上下文": "compact",
	// session recovery & export
	"/resume": "resume", "/continue": "resume", "恢复": "resume", "恢复会话": "resume",
	"/export": "export", "导出": "export", "导出对话": "export",
	// configuration
	"/model": "model", "模型": "model",
	"/think": "think", "深度思考": "think", "思考": "think",
	"/dir": "dir", "/cd": "dir", "/cwd": "dir", "/pwd": "dir", "目录": "dir",
	"/mode": "mode", "权限": "mode", "模式": "mode",
	"/timeout": "timeout", "时限": "timeout", "超时": "timeout",
	// Claude Code management commands
	"/agents": "agents", "/agent": "agents", "子代理": "agents", "代理": "agents",
	"/mcp":    "mcp",
	"/memory": "memory", "/mem": "memory", "记忆": "memory",
	"/doctor": "doctor", "/health": "doctor", "诊断": "doctor", "健康": "doctor",
	// Claude Code feature shortcuts
	"/review": "review", "评审": "review", "审查": "review",
	"/diff": "diff", "改动": "diff",
	"/explain": "explain", "解释": "explain",
	"/web": "web", "搜索": "web", "联网": "web",
	"/init": "init",
	// info & usage
	"/usage": "usage", "额度": "usage", "用量": "usage",
	"/cost": "cost", "花费": "cost",
	"/status": "status", "/stat": "status", "状态": "status",
	"/whoami": "whoami", "/me": "whoami", "我是谁": "whoami",
	"/sessions": "sessions", "/conv": "sessions", "会话": "sessions",
	"/version": "version", "/ver": "version", "版本": "version",
	"/ping": "ping",
	"/help": "help", "/h": "help", "/?": "help", "帮助": "help", "菜单": "help",
}

// helpCommand is one command shown in /help: desc is the few-character label
// used inline by the compact view; detail is the fuller usage line the detailed
// view shows (falls back to desc when empty).
type helpCommand struct{ cmd, desc, detail string }

// helpGroup is a labelled set of commands.
type helpGroup struct {
	title string
	cmds  []helpCommand
}

// helpGroups is the canonical command list rendered by /help, grouped by area.
// Descriptions are kept to a few characters so they read cleanly both inline
// (compact view) and one-per-line (detailed view).
var helpGroups = []helpGroup{
	{"💬 对话", []helpCommand{
		{"/new", "新对话", "清空上下文，重新开始"},
		{"/retry", "重试上条", "重新执行上一条消息"},
		{"/stop", "中断任务", "停止正在运行的任务"},
		{"/compact", "压缩续聊", "把长对话压成交接摘要后继续；/compact <侧重点> 可指定保留重点"},
	}},
	{"⏪ 会话", []helpCommand{
		{"/resume", "恢复历史", "列出历史会话；/resume <序号或 id 前缀> 接回"},
		{"/export", "导出记录", "把本会话导出为 Markdown 文件发给你"},
		{"/sessions", "会话列表", "所有活跃会话一览"},
		{"/status", "运行状态", "会话 / 模型 / 当前任务实时状态"},
	}},
	{"⚙️ 配置", []helpCommand{
		{"/model", "切换模型", "看当前与可选模型；/model <全名> 切换，default 恢复"},
		{"/think", "深度思考", "下一条回复用深度思考"},
		{"/dir", "工作目录", "/dir <路径> 切换，default 恢复"},
		{"/mode", "权限模式", "default / plan / acceptEdits / bypass"},
		{"/timeout", "单轮时限", "/timeout <分钟> 设置本会话时限，default 恢复"},
	}},
	{"🧩 管理", []helpCommand{
		{"/agents", "子代理", "列出可用的后台子代理"},
		{"/mcp", "MCP", "列出配置的 MCP 服务器"},
		{"/memory", "记忆", "查看加载的 CLAUDE.md 记忆"},
		{"/doctor", "诊断", "环境健康检查（CLI / 认证 / 网关）"},
	}},
	{"⚡ 快捷", []helpCommand{
		{"/review", "代码评审", "评审当前代码改动"},
		{"/diff", "git 改动", "总结 git status 与 diff"},
		{"/explain", "解释内容", "/explain <代码或问题>"},
		{"/web", "联网搜索", "/web <问题>，给出带来源的答案"},
		{"/init", "项目文档", "生成 / 更新项目 CLAUDE.md"},
	}},
	{"📊 信息", []helpCommand{
		{"/usage", "额度", "订阅用量与重置时间"},
		{"/cost", "花费", "上一条回复的耗时与成本"},
		{"/whoami", "身份", "我的 open_id"},
		{"/version", "版本", "网关版本与运行时长"},
		{"/ping", "连通", "连通性测试"},
		{"/help", "帮助", "/help 简版；/help all 本详细版"},
	}},
}

// helpText (compact, the /help default) fits the whole menu in one screen: one
// list item per GROUP with its commands inline, separated by "·". helpFullText
// (/help all) is the airy one-command-per-line version for reading descriptions.
// QQ Markdown has no code blocks/tables/monospace and a bare newline is not a
// reliable line break, so both views are built from bold labels + Markdown lists
// — the only dependable per-line primitives. Built once.
var (
	helpText     = buildHelpText()
	helpFullText = buildHelpFullText()
)

// sendHelp sends the command menu: compact by default, detailed for
// "/help all" (and Chinese equivalents).
func (g *Gateway) sendHelp(ctx context.Context, r *responder, arg string) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "all", "full", "详细", "全部", "更多":
		_ = r.Send(ctx, helpFullText)
	default:
		_ = r.Send(ctx, helpText)
	}
}

func buildHelpText() string {
	var b strings.Builder
	b.WriteString("## 🤖 Claude Code · QQ\n\n直接发需求即可，命令只是辅助：\n")
	for _, g := range helpGroups {
		b.WriteString("\n- **" + g.title + "**　")
		for i, c := range g.cmds {
			if i > 0 {
				b.WriteString(" · ")
			}
			b.WriteString(c.cmd + " " + c.desc)
		}
	}
	b.WriteString("\n\n👉 详细版发 **/help all**")
	return b.String()
}

func buildHelpFullText() string {
	var b strings.Builder
	b.WriteString("## 🤖 Claude Code · QQ · 全部命令\n\n可带参数的命令，不带参数发送就是查看当前状态：")
	for _, g := range helpGroups {
		b.WriteString("\n\n**" + g.title + "**")
		for _, c := range g.cmds {
			detail := c.detail
			if detail == "" {
				detail = c.desc
			}
			b.WriteString("\n- " + c.cmd + " — " + detail)
		}
	}
	b.WriteString("\n\n返回简版发 **/help**")
	return b.String()
}

// maxPassiveReplies is QQ's cap on passive replies per inbound message (the C2C
// passive-reply window itself is 60 minutes).
const maxPassiveReplies = 5

// longTurnNotice is how long a turn runs before the gateway sends a
// "still working" reassurance; veryLongTurnNotice adds one more progress update
// for genuinely long tasks. Two notices at most: the rest of the passive-reply
// budget is reserved for the actual result. Both are well inside QQ's
// 60-minute passive window.
const (
	longTurnNotice     = 90 * time.Second
	veryLongTurnNotice = 10 * time.Minute
)

// progressNotice describes what a long-running turn is doing right now, using
// the session's live tool telemetry — so the wait feels like watching Claude
// Code work, not like a dead line.
func progressNotice(sess *session.Session, elapsed time.Duration, more bool) string {
	tool, calls, _ := sess.ToolActivity()
	msg := fmt.Sprintf("🟡 还在干活（已 %s", elapsed.Round(time.Second))
	if calls > 0 {
		msg += fmt.Sprintf("，%d 次工具调用，正在用 %s", calls, tool)
	}
	msg += "）…结果一出来我就发你。"
	if more {
		msg += " 想中断可发 /stop。"
	}
	return msg
}

// runTurn executes one Claude Code turn for a conversation and sends the reply,
// including any inbound attachments (downloaded for Claude to read) and any
// outbound media Claude asks to send.
func (g *Gateway) runTurn(ctx context.Context, r *responder, key, text string, atts []qq.MessageAttachment) {
	sess := g.sessions.Get(key)
	// Contain any panic in this turn: log it and tell the user, rather than letting it
	// crash the whole gateway. Registered first so it runs last — after Unlock/EndTurn
	// below have released the locks, making the user-facing Send safe.
	defer func() {
		if rec := recover(); rec != nil {
			g.logger.Printf("[gateway] [%s] turn PANIC: %v\n%s", key, rec, debug.Stack())
			_ = r.Send(context.Background(), "⚠️ 内部出错了（已记录日志）。请重试，或发送 /new 重开对话。")
		}
	}()
	// Messages for one conversation are serialized by sess.Lock(); a message that
	// arrives while a turn is in flight waits here. Tell the user it is queued so the
	// wait doesn't look like a dropped message or a crash.
	if sess.Running() {
		_ = r.Send(ctx, "⏳ 上一条还在跑，这条先排队了，等它跑完我马上接着处理。")
	}
	sess.Lock()
	defer sess.Unlock()

	// Make the turn cancellable so /stop can interrupt it.
	turnCtx, cancel := context.WithCancel(ctx)
	sess.BeginTurn(cancel)
	defer func() {
		sess.EndTurn()
		cancel()
	}()

	// QQ passive replies are capped (5 per inbound message), so we can't truly
	// heartbeat a long task. Send at most two progress notices — an early one so
	// the turn doesn't look dead, and a later one for genuinely long tasks — each
	// reporting the turn's live tool activity. Started AFTER the queue wait above
	// (the timers measure this turn's work, not time spent waiting for its
	// predecessor). One-shot; Stop() cancels them if the turn finishes first.
	turnStart := time.Now()
	ping := time.AfterFunc(longTurnNotice, func() {
		_ = r.Send(context.Background(), progressNotice(sess, time.Since(turnStart), false))
	})
	defer ping.Stop()
	ping2 := time.AfterFunc(veryLongTurnNotice, func() {
		_ = r.Send(context.Background(), progressNotice(sess, time.Since(turnStart), true))
	})
	defer ping2.Stop()

	// Apply a pending /think request, then download any attachments so Claude
	// can read them as local files. The attachment note is kept separate so
	// /retry can replay the message WITH its attachments (the files persist on
	// disk) instead of silently dropping them.
	retryText := text
	if note := g.materializeAttachments(turnCtx, key, atts); note != "" {
		retryText = strings.TrimSpace(text + "\n\n" + note)
	}
	prompt := retryText
	if sess.TakeThinkNext() {
		prompt = "ultrathink\n\n" + prompt
	}

	resuming := sess.GetSessionID()
	// A compact seed carries the previous context into this fresh session. Only a
	// fresh turn consumes it (a resuming session already has its context); it is
	// re-armed on failure below so an errored turn doesn't eat the summary.
	seed := ""
	if resuming == "" {
		if seed = sess.TakeSeed(); seed != "" {
			prompt = "以下是此前对话压缩后的交接摘要，请基于它无缝继续（不必复述摘要内容）：\n\n" + seed +
				"\n\n---\n\n现在，操作者说：\n\n" + prompt
		}
	}

	if g.cfg.ThinkingMessage != "" {
		if err := r.Send(turnCtx, g.cfg.ThinkingMessage); err != nil {
			g.logger.Printf("[gateway] send thinking message: %v", err)
		}
	}

	// Capture the session generation now; if /new (or an idle reset) clears the
	// session while this turn runs, the generation changes and we won't write our
	// session id back at the end (see SetSessionIDIfGen).
	gen := sess.ClaudeGen()
	var timeout time.Duration
	if min := sess.GetTimeoutMin(); min > 0 {
		timeout = time.Duration(min) * time.Minute
	}
	g.logger.Printf("[gateway] [%s] running claude turn (resume=%t seed=%t)", key, resuming != "", seed != "")
	res, err := g.bridge.Run(turnCtx, claude.Request{
		SessionID:      resuming,
		Prompt:         prompt,
		Model:          sess.GetModel(),
		WorkDir:        sess.GetWorkDir(),
		PermissionMode: sess.GetMode(),
		Timeout:        timeout,
		OnActivity: func(tool string) {
			sess.NoteTool(tool)
			g.logger.Printf("[gateway] [%s] tool: %s", key, tool)
		},
	})
	if err != nil {
		if turnCtx.Err() == context.Canceled {
			g.logger.Printf("[gateway] [%s] turn cancelled", key)
			return
		}
		// A killed/failed turn may still have reported a session id mid-stream. Keep it
		// so the conversation can be resumed; only when we have none and were resuming
		// do we clear the stored id (it was likely stale and would re-fail forever).
		if res != nil && res.SessionID != "" {
			sess.SetSessionID(res.SessionID)
			g.logger.Printf("[gateway] [%s] kept session id after error for resume", key)
		} else if resuming != "" {
			sess.ClearClaude()
			g.logger.Printf("[gateway] [%s] cleared possibly-stale session id after error", key)
		} else if seed != "" && (res == nil || res.SessionID == "") {
			// The seeded turn never took hold — re-arm the compact summary so the
			// context it carries isn't lost to a transient failure.
			sess.SetSeed(seed)
		}
		g.persist()
		g.logger.Printf("[gateway] [%s] claude error: %v", key, err)
		if errors.Is(err, claude.ErrTurnTimeout) {
			g.deliverOrQueue(ctx, r, sess, key, "⏳ 这条任务跑满了时限被中止。进度已保存，直接回我一句「继续」就能接着干。")
		} else {
			g.deliverOrQueue(ctx, r, sess, key, "⚠️ 出错了 (Claude error): "+short(err.Error()))
		}
		return
	}
	// The CLI exits 0 even when the turn itself errored (e.g. a bad --model 404s, or an
	// auth problem): is_error marks that. Surface it as an error and do NOT advance the
	// session — persisting the id/turn here is what previously wedged the conversation.
	if res.IsError {
		g.logger.Printf("[gateway] [%s] claude returned is_error: %s", key, short(res.Text))
		msg := "⚠️ Claude 返回错误：" + strings.TrimSpace(res.Text)
		if strings.Contains(strings.ToLower(res.Text), "model") {
			msg += "\n\n可能是模型设置问题，试试 /model default 恢复默认模型。"
		}
		g.deliverOrQueue(ctx, r, sess, key, msg)
		return
	}
	if res.SessionID != "" {
		if !sess.SetSessionIDIfGen(res.SessionID, gen) {
			g.logger.Printf("[gateway] [%s] session was reset mid-turn; leaving the cleared context cleared", key)
		}
	}
	sess.IncTurn()
	sess.RecordTurn(retryText, res.CostUSD, res.DurationMS)
	g.addUsage(res.CostUSD)
	g.persist()

	reply := strings.TrimSpace(res.Text)
	if reply == "" {
		reply = "(empty response)"
	}
	g.deliverOrQueue(ctx, r, sess, key, reply)
}

// PushToOperator delivers a proactive (active-push) message to the configured
// operator, used by the local notify endpoint (e.g. the trading bot's close
// reports). It pushes actively from the start (there is no inbound message to
// reply to); if the active push is unavailable (e.g. QQ active-message quota),
// the text is queued on the operator's session and flushed on their next inbound
// message — so a settlement notice is never silently lost. Returns nil once the
// message is either delivered or safely queued.
func (g *Gateway) PushToOperator(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty notify text")
	}
	openID := strings.TrimSpace(g.cfg.NotifyOpenID)
	if openID == "" && len(g.cfg.AllowedUsers) > 0 {
		openID = g.cfg.AllowedUsers[0]
	}
	if openID == "" {
		return fmt.Errorf("no notify target (set gateway.notify_open_id or allowed_users)")
	}
	key := "c2c:" + openID
	sess := g.sessions.Get(key)
	r := &responder{client: g.client, userOpenID: openID, asMarkdown: g.cfg.ReplyAsMarkdown, nextSeq: sess.NextSeq}
	r.GoActive()
	if err := g.deliver(ctx, r, key, text); err != nil {
		sess.QueuePending(text)
		g.persist()
		g.logger.Printf("[gateway] [%s] notify active push failed (%v); queued for next inbound", key, err)
		return nil
	}
	g.logger.Printf("[gateway] [%s] notify delivered via active push", key)
	return nil
}

// errPassiveBudget marks a delivery that could not even start because earlier
// sends on this inbound message (thinking notice, queue notice, long-turn ping,
// pending flushes) already used up QQ's 5-passive-reply allowance. The caller
// escalates exactly as for a failed send (active push, then queue).
var errPassiveBudget = errors.New("passive-reply budget exhausted for this message")

// deliver sends Claude's reply: extracts outbound media directives, sends the
// text (as a file when it is too long to fit the passive-reply budget), then
// delivers each media item — all within QQ's 5-passive-reply cap. It returns a
// non-nil error if the text itself could not be sent (so the caller can fall back
// to active push or queue it for next time); media failures are non-fatal.
func (g *Gateway) deliver(ctx context.Context, r *responder, key, text string) error {
	text, media := extractSendDirectives(text)

	// The 5-reply cap is per inbound message, and this responder may already have
	// spent part of it on notices (thinking message, queued notice, long-turn
	// ping, pending flushes) — count what's actually left instead of assuming a
	// fresh window. Active pushes are not subject to the per-message cap (they
	// are quota'd monthly instead), so an active responder keeps the full budget
	// as a chunk-count bound.
	budget := maxPassiveReplies
	if !r.Active() {
		budget = maxPassiveReplies - r.SentCount()
		if budget < 1 {
			return errPassiveBudget
		}
	}
	chunks := splitMessage(text, g.cfg.MaxReplyChars)
	textBudget := budget - len(media)
	if textBudget < 1 {
		textBudget = 1
	}

	if text != "" && len(chunks) > textBudget {
		// Too long to fit: deliver the full text as a file (C2C always supports upload).
		if g.cfg.LongRepliesAsFile() {
			if path, err := g.writeReplyFile(key, text); err == nil {
				media = append([]mediaItem{{kind: qq.FileTypeFile, path: path}}, media...)
				head := truncateRunes(text, g.cfg.MaxReplyChars-48)
				chunks = []string{head + "\n\n（完整内容见附件 / full output attached as a file）"}
			} else {
				g.logger.Printf("[gateway] write reply file: %v", err)
				chunks = g.capChunks(chunks, textBudget)
			}
		} else {
			chunks = g.capChunks(chunks, textBudget)
		}
	}

	sent := 0
	if text != "" {
		for _, c := range chunks {
			if sent >= budget {
				break
			}
			if err := r.Send(ctx, c); err != nil {
				g.logger.Printf("[gateway] send reply chunk: %v", err)
				return err
			}
			sent++
		}
	}

	for _, m := range media {
		if sent >= budget {
			g.logger.Printf("[gateway] media %s dropped (passive-reply budget reached)", m.ref())
			continue
		}
		if err := r.SendMedia(ctx, m.kind, m.path, m.url); err != nil {
			g.logger.Printf("[gateway] send media %s: %v", m.ref(), err)
			// Fall back to telling the user where it is.
			if sent < budget {
				if err2 := r.Send(ctx, "📎 "+m.ref()); err2 == nil {
					sent++
				}
			}
			continue
		}
		sent++
	}
	return nil
}

// deliverOrQueue delivers a turn's reply without wasting the scarce active-push
// quota. Per the QQ C2C docs a passive reply is valid for 60 minutes (5 per
// inbound message) while ACTIVE pushes are capped at 4 PER MONTH — so we always
// try passive first (it covers all but the longest turns), fall back to a single
// active push only if passive fails (window truly expired), and finally queue the
// text on the session for delivery on the next inbound message. Every path
// guarantees the reply is never silently lost.
func (g *Gateway) deliverOrQueue(ctx context.Context, r *responder, sess *session.Session, key, text string) {
	if err := g.deliver(ctx, r, key, text); err == nil {
		return
	} else {
		g.logger.Printf("[gateway] [%s] passive delivery failed (%v); trying one active push", key, err)
	}
	r.GoActive()
	if err := g.deliver(ctx, r, key, text); err == nil {
		g.logger.Printf("[gateway] [%s] reply delivered via active push (passive window expired)", key)
		return
	} else {
		sess.QueuePending(text)
		g.persist()
		g.logger.Printf("[gateway] [%s] active push unavailable (%v); queued for next inbound message", key, err)
	}
}

// capChunks collapses chunks beyond max into the last allowed chunk so output
// stays within the passive-reply limit (used only when file delivery is off).
func (g *Gateway) capChunks(chunks []string, max int) []string {
	if max < 1 {
		max = 1
	}
	if len(chunks) <= max {
		return chunks
	}
	head := chunks[:max-1]
	tail := strings.Join(chunks[max-1:], "\n")
	return append(head, truncateRunes(tail, g.cfg.MaxReplyChars))
}
