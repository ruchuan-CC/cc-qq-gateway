// Package gateway wires QQ Bot events to a local Claude Code session and routes
// Claude's replies back to the originating QQ conversation.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chenhg5/cc-qq-gateway/internal/claude"
	"github.com/chenhg5/cc-qq-gateway/internal/config"
	"github.com/chenhg5/cc-qq-gateway/internal/qq"
	"github.com/chenhg5/cc-qq-gateway/internal/session"
)

// Version is the gateway build version, surfaced via /version.
const Version = "0.3.0"

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

// flushPending delivers any replies queued from an earlier turn that outran the
// passive-reply window. Delivered as passive replies to the current message, which
// has a fresh window. Sent best-effort; a delivery failure re-queues nothing (the
// next message will retry whatever this call leaves behind via re-queue below).
func (g *Gateway) flushPending(ctx context.Context, r *responder, key string) {
	pending := g.sessions.Get(key).TakePending()
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
		// Unknown slash-commands are reported rather than sent to Claude, so a
		// typo doesn't silently turn into a prompt.
		if strings.HasPrefix(name, "/") {
			_ = r.Send(ctx, "❓ 未知命令 `"+name+"`，发送 **`/help`** 查看可用命令。")
			return true
		}
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
		_ = r.Send(ctx, "✅ **已开启新对话**，上下文已清空。")
	case "model":
		g.cmdModel(ctx, r, key, arg)
	case "dir":
		g.cmdCwd(ctx, r, key, arg)
	case "mode":
		g.cmdMode(ctx, r, key, arg)
	case "usage":
		go func() { _ = r.Send(context.Background(), g.usageText()) }()
	case "mcp":
		go g.runManaged(r, key, "🔌 MCP 服务器", "mcp", "list")
	case "agents":
		go g.runManaged(r, key, "🤖 子代理 (agents)", "agents", "--json")
	case "memory":
		go g.cmdMemory(r, key)
	case "doctor":
		go g.cmdDoctor(r, key)
	case "think":
		g.sessions.Get(key).SetThinkNext()
		_ = r.Send(ctx, "🧠 **下一条回复将进行深度思考。**")
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
		_ = r.Send(ctx, "**🪪 你的身份**\n"+r.identity()+
			"\n\n把这个 open_id 填入配置的 `allowed_users` 即可锁定操作者。")
	case "version":
		_ = r.Send(ctx, fmt.Sprintf("**🏷️ 版本** cc-qq-gateway v%s · 运行 %s", Version, g.uptime()))
	case "ping":
		_ = r.Send(ctx, "🏓 pong · 运行 "+g.uptime())
	case "sessions":
		_ = r.Send(ctx, g.sessionsText())
	case "help":
		_ = r.Send(ctx, helpText)
	default:
		return false
	}
	return true
}

// modelHint lists the model names the CLI accepts, shown when a name is unknown.
const modelHint = "可用：`opus` / `sonnet` / `haiku` / `fable`（或完整 id，如 `claude-opus-4-8[1m]`）。恢复默认：`/model default`"

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
				cur = "default (CLI)"
			}
			cur += " (default)"
		}
		_ = r.Send(ctx, "**🧠 模型** `"+cur+"`\n切换：`/model <名称>`\n"+modelHint)
		return
	}
	canon, ok := claude.NormalizeModel(arg)
	if !ok {
		_ = r.Send(ctx, "⚠️ 无法识别的模型名 `"+arg+"`。\n"+modelHint)
		return
	}
	sess.SetModel(canon)
	if canon == "" {
		_ = r.Send(ctx, "**🧠 模型** 已恢复默认。")
		return
	}
	_ = r.Send(ctx, "**🧠 模型** 已切换为 `"+canon+"`")
}

// cmdCwd shows or sets the per-conversation working directory override.
func (g *Gateway) cmdCwd(ctx context.Context, r *responder, key, arg string) {
	sess := g.sessions.Get(key)
	if arg == "" {
		cur := sess.GetWorkDir()
		if cur == "" {
			cur = g.bridge.DefaultWorkDir()
			if cur == "" {
				cur = "(claude default)"
			}
			cur += " (default)"
		}
		_ = r.Send(ctx, "**📁 工作目录** `"+cur+"`\n切换：`/dir <路径>` · 恢复默认：`/dir default`")
		return
	}
	if strings.EqualFold(arg, "default") || strings.EqualFold(arg, "reset") {
		sess.SetWorkDir("")
		_ = r.Send(ctx, "**📁 工作目录** 已恢复默认。")
		return
	}
	sess.SetWorkDir(arg)
	_ = r.Send(ctx, "**📁 工作目录** 已切换为 `"+arg+"`")
}

// cmdMode shows or sets the per-conversation permission mode.
func (g *Gateway) cmdMode(ctx context.Context, r *responder, key, arg string) {
	sess := g.sessions.Get(key)
	if arg == "" {
		cur := sess.GetMode()
		if cur == "" {
			cur = "默认（按网关配置）"
		}
		_ = r.Send(ctx, "**🔐 权限模式** `"+cur+"`\n可选：`default` `plan` `acceptEdits` `bypass`\n用法：`/mode <名称>`")
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
		_ = r.Send(ctx, "❓ 未知模式 `"+arg+"`，可选：default / plan / acceptEdits / bypass")
		return
	}
	sess.SetMode(norm)
	_ = r.Send(ctx, "**🔐 权限模式** 已切换为 `"+norm+"`")
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
		_ = r.Send(ctx, "用法：`/"+name+" <内容>`")
		return
	}
	prompt := tmpl
	if arg != "" {
		prompt = tmpl + " " + arg
	}
	go g.runTurn(context.Background(), r, key, prompt, nil)
}

// cstZone displays reset times in China Standard Time for the operator.
var cstZone = time.FixedZone("CST", 8*3600)

// runManaged runs a claude management subcommand and delivers its output.
func (g *Gateway) runManaged(r *responder, key, label string, args ...string) {
	out, err := g.bridge.RunCLI(context.Background(), args...)
	if out == "" {
		if err != nil {
			out = "执行失败：" + short(err.Error())
		} else {
			out = "（无输出）"
		}
	}
	g.deliver(context.Background(), r, key, "**"+label+"**\n```\n"+out+"\n```")
}

// cmdMemory shows the CLAUDE.md memory files Claude loads (global + project).
func (g *Gateway) cmdMemory(r *responder, key string) {
	home, _ := os.UserHomeDir()
	candidates := []string{filepath.Join(home, ".claude", "CLAUDE.md")}
	if wd := g.bridge.DefaultWorkDir(); wd != "" {
		candidates = append(candidates, filepath.Join(wd, "CLAUDE.md"))
	}
	var b strings.Builder
	b.WriteString("**🧠 记忆 (CLAUDE.md)**\n")
	found := false
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		found = true
		b.WriteString("\n`" + p + "`\n```\n" + strings.TrimSpace(string(data)) + "\n```\n")
	}
	if !found {
		b.WriteString("（暂无 CLAUDE.md 记忆文件；让我“记住…”即可创建）")
	}
	g.deliver(context.Background(), r, key, b.String())
}

// cmdDoctor reports a practical environment health summary (claude doctor itself
// is interactive-only, so this gives the equivalent useful checks).
func (g *Gateway) cmdDoctor(r *responder, key string) {
	ver, _ := g.bridge.RunCLI(context.Background(), "--version")
	plan := "未知"
	// Keep cell values emoji-free: emoji width is renderer-dependent and would
	// misalign the monospace table. Emoji live in the bold header outside the fence.
	auth := "未认证"
	if u, err := claude.FetchUsage(context.Background()); err == nil {
		auth = "已认证"
		if u.Plan != "" {
			plan = prettyPlan(u.Plan)
		}
	}
	rows := [][]string{
		{"Claude CLI", strings.TrimSpace(ver)},
		{"订阅认证", auth},
		{"订阅套餐", plan},
		{"网关版本", "v" + Version},
		{"运行时长", g.uptime()},
		{"工具权限", authorityLabel(g.bridge.FullAuthority())},
	}
	_ = r.Send(context.Background(), "**🩺 环境诊断**\n\n"+renderTable([]string{"检查项", "结果"}, rows))
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
	rows := make([][]string, 0, 4)
	addWindow := func(label string, w claude.Window) {
		if !w.Has {
			return
		}
		reset := "—"
		if !w.ResetsAt.IsZero() {
			reset = fmt.Sprintf("%s后（%s）", humanDur(time.Until(w.ResetsAt)), w.ResetsAt.In(cstZone).Format("01-02 15:04"))
		}
		rows = append(rows, []string{label, fmt.Sprintf("%.0f%%", w.Utilization), reset})
	}
	addWindow("5 小时", u.FiveHour)
	addWindow("7 天", u.SevenDay)
	addWindow("7天·Opus", u.Opus)
	addWindow("7天·Sonnet", u.Sonnet)

	head := "**📊 订阅用量**"
	if u.Plan != "" {
		head += " · " + prettyPlan(u.Plan)
	}
	return head + "\n\n" + renderTable([]string{"窗口", "已用", "重置"}, rows)
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
	if s.Running() {
		running = "运行中"
	}
	if d := s.RunningFor(); d > 0 {
		running = "运行中 · 已跑 " + d.Round(time.Second).String()
	}
	rows := [][]string{
		{"会话", status},
		{"模型", model},
		{"目录", workDir},
		{"权限", authority},
		{"任务", running},
		{"轮数", fmt.Sprintf("%d", s.TurnCount())},
		{"运行", g.uptime() + " · v" + Version},
	}
	return "**📊 运行状态**\n\n" + renderTable([]string{"项", "值"}, rows)
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
	rows := make([][]string, 0, len(snaps))
	for _, s := range snaps {
		state := "空闲"
		if s.Running {
			state = "运行中"
		}
		conn := "新会话"
		if s.Active {
			conn = "已连接"
		}
		rows = append(rows, []string{
			short(s.Key), conn, state,
			fmt.Sprintf("%d", s.Turns),
			time.Since(s.LastActive).Round(time.Second).String(),
		})
	}
	head := fmt.Sprintf("**💬 活跃会话** %d 个 · 累计 %d 轮 · $%.4f", len(snaps), turns, cost)
	return head + "\n\n" + renderTable([]string{"会话", "连接", "状态", "轮数", "闲置"}, rows)
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
	// configuration
	"/model": "model", "模型": "model",
	"/think": "think", "深度思考": "think", "思考": "think",
	"/dir": "dir", "/cd": "dir", "/cwd": "dir", "/pwd": "dir", "目录": "dir",
	"/mode": "mode", "权限": "mode", "模式": "mode",
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

// helpCommand is one row of the /help table.
type helpCommand struct{ group, cmd, desc string }

// helpCommands is the canonical command list rendered by /help, grouped by area.
var helpCommands = []helpCommand{
	{"对话", "/new", "开启新对话（清空上下文）"},
	{"对话", "/retry", "重做上一条消息"},
	{"对话", "/stop", "中断当前任务"},
	{"配置", "/model", "查看 / 切换模型"},
	{"配置", "/think", "下一条深度思考"},
	{"配置", "/dir", "查看 / 切换工作目录"},
	{"配置", "/mode", "权限模式"},
	{"Claude", "/agents", "后台子代理"},
	{"Claude", "/mcp", "MCP 服务器"},
	{"Claude", "/memory", "查看记忆 CLAUDE.md"},
	{"Claude", "/doctor", "环境诊断"},
	{"快捷", "/review", "代码评审"},
	{"快捷", "/diff", "查看 git 改动"},
	{"快捷", "/explain", "解释代码 / 内容"},
	{"快捷", "/web", "联网搜索"},
	{"快捷", "/init", "生成 CLAUDE.md"},
	{"信息", "/usage", "用量额度"},
	{"信息", "/cost", "上次花费"},
	{"信息", "/status", "运行状态"},
	{"信息", "/sessions", "活跃会话"},
	{"信息", "/whoami", "我的 open_id"},
	{"信息", "/version", "版本信息"},
	{"信息", "/ping", "连通测试"},
	{"信息", "/help", "显示本帮助"},
}

// helpText is the rendered /help message: a Markdown intro plus a monospace,
// box-drawn command table inside a fenced code block. QQ does NOT render GFM pipe
// tables, but it does render code blocks in a monospace font, so an aligned ASCII
// table is the layout that actually looks like a table on QQ (and degrades to
// still-readable framed text if markdown falls back to plain). Columns are padded
// by display width (CJK = 2 cells) so Chinese and ASCII line up. Built once.
var helpText = buildHelpText()

func buildHelpText() string {
	headers := []string{"类别", "命令", "说明"}
	rows := make([][]string, 0, len(helpCommands))
	for _, c := range helpCommands {
		rows = append(rows, []string{c.group, c.cmd, c.desc})
	}
	w := colWidths(headers, rows)

	var b strings.Builder
	b.WriteString("**🤖 Claude Code · QQ** —— 直接说需求即可，命令可选：\n\n```\n")
	b.WriteString(tableRule(w, "┌", "┬", "┐") + "\n")
	b.WriteString(tableRow(headers, w) + "\n")
	b.WriteString(tableRule(w, "├", "┼", "┤") + "\n")
	prev := ""
	for i, c := range helpCommands {
		// Separate groups with a rule and show the group label only on its first
		// row, so the table stays scannable.
		if i > 0 && c.group != prev {
			b.WriteString(tableRule(w, "├", "┼", "┤") + "\n")
		}
		label := c.group
		if c.group == prev {
			label = ""
		}
		b.WriteString(tableRow([]string{label, c.cmd, c.desc}, w) + "\n")
		prev = c.group
	}
	b.WriteString(tableRule(w, "└", "┴", "┘") + "\n```\n")
	b.WriteString("中文别名也可用（如「新对话」「停止」「状态」「联网」）。")
	return b.String()
}

// maxPassiveReplies is QQ's cap on passive replies per inbound message (the C2C
// passive-reply window itself is 60 minutes).
const maxPassiveReplies = 5

// longTurnNotice is how long a turn runs before the gateway sends a single
// "still working" reassurance. Kept well inside QQ's passive-reply window.
const longTurnNotice = 90 * time.Second

// runTurn executes one Claude Code turn for a conversation and sends the reply,
// including any inbound attachments (downloaded for Claude to read) and any
// outbound media Claude asks to send.
func (g *Gateway) runTurn(ctx context.Context, r *responder, key, text string, atts []qq.MessageAttachment) {
	sess := g.sessions.Get(key)
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

	// QQ passive replies are capped (5 per inbound message, ~5-minute window), so we
	// can't truly heartbeat a long task. Send a single reassurance once a turn has
	// clearly become long-running, so it doesn't look dead. One-shot; Stop() cancels
	// it if the turn finishes first.
	ping := time.AfterFunc(longTurnNotice, func() {
		_ = r.Send(context.Background(), "🟡 还在干活，这条任务比较久，跑完我会把结果发来，稍等。")
	})
	defer ping.Stop()

	// Apply a pending /think request, then download any attachments so Claude
	// can read them as local files.
	prompt := text
	if sess.TakeThinkNext() {
		prompt = "ultrathink\n\n" + prompt
	}
	if note := g.materializeAttachments(turnCtx, key, atts); note != "" {
		prompt = strings.TrimSpace(prompt + "\n\n" + note)
	}

	if g.cfg.ThinkingMessage != "" {
		if err := r.Send(turnCtx, g.cfg.ThinkingMessage); err != nil {
			g.logger.Printf("[gateway] send thinking message: %v", err)
		}
	}

	resuming := sess.GetSessionID()
	g.logger.Printf("[gateway] [%s] running claude turn (resume=%t)", key, resuming != "")
	res, err := g.bridge.Run(turnCtx, claude.Request{
		SessionID:      resuming,
		Prompt:         prompt,
		Model:          sess.GetModel(),
		WorkDir:        sess.GetWorkDir(),
		PermissionMode: sess.GetMode(),
		OnActivity: func(tool string) {
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
		}
		g.logger.Printf("[gateway] [%s] claude error: %v", key, err)
		if turnCtx.Err() == context.DeadlineExceeded {
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
			msg += "\n\n可能是模型设置问题，试试 `/model default` 恢复默认模型。"
		}
		g.deliverOrQueue(ctx, r, sess, key, msg)
		return
	}
	if res.SessionID != "" {
		sess.SetSessionID(res.SessionID)
	}
	sess.IncTurn()
	sess.RecordTurn(text, res.CostUSD, res.DurationMS)
	g.addUsage(res.CostUSD)

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
		g.logger.Printf("[gateway] [%s] notify active push failed (%v); queued for next inbound", key, err)
		return nil
	}
	g.logger.Printf("[gateway] [%s] notify delivered via active push", key)
	return nil
}

// deliver sends Claude's reply: extracts outbound media directives, sends the
// text (as a file when it is too long to fit the passive-reply budget), then
// delivers each media item — all within QQ's 5-passive-reply cap. It returns a
// non-nil error if the text itself could not be sent (so the caller can fall back
// to active push or queue it for next time); media failures are non-fatal.
func (g *Gateway) deliver(ctx context.Context, r *responder, key, text string) error {
	text, media := extractSendDirectives(text)

	budget := maxPassiveReplies
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
