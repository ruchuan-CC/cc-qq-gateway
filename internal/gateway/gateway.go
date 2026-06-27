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

	allowedGroups map[string]bool
	allowedUsers  map[string]bool

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
		client:        client,
		bridge:        bridge,
		sessions:      sessions,
		cfg:           cfg,
		logger:        logger,
		allowedGroups: toSet(cfg.AllowedGroups),
		allowedUsers:  toSet(cfg.AllowedUsers),
		startedAt:     time.Now(),
	}
	return g
}

// restricted reports whether any allowlist is configured. In restricted mode the
// bot serves only whitelisted C2C users and explicitly-allowlisted groups, and
// ignores guild channels and guild DMs entirely (those surfaces have no per-user
// allowlist, so leaving them open would defeat "lock to my id").
func (g *Gateway) restricted() bool {
	return g.allowedUsers != nil || g.allowedGroups != nil
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

// HandleEvent is the qq.EventHandler. It classifies the event and, for inbound
// conversational messages, dispatches a Claude turn asynchronously.
func (g *Gateway) HandleEvent(ctx context.Context, p *qq.Payload) {
	switch p.Type {
	case qq.EventGroupAtMessageCreate:
		var m qq.GroupAtMessage
		if err := json.Unmarshal(p.Data, &m); err != nil {
			g.logger.Printf("[gateway] decode group message: %v", err)
			return
		}
		if g.allowedGroups != nil {
			if !g.allowedGroups[m.GroupOpenID] {
				g.logger.Printf("[gateway] ignoring group message from non-allowlisted group %s", m.GroupOpenID)
				return
			}
		} else if g.restricted() {
			g.logger.Printf("[gateway] ignoring group message (locked to allowed_users; no groups allowlisted)")
			return
		}
		r := &responder{
			client: g.client, kind: kindGroup,
			groupOpenID: m.GroupOpenID, msgID: m.ID,
			asMarkdown: g.cfg.ReplyAsMarkdown,
		}
		g.dispatch(ctx, r, cleanContent(m.Content), m.Attachments)

	case qq.EventC2CMessageCreate:
		var m qq.C2CMessage
		if err := json.Unmarshal(p.Data, &m); err != nil {
			g.logger.Printf("[gateway] decode c2c message: %v", err)
			return
		}
		if g.allowedUsers != nil {
			if !g.allowedUsers[m.Author.UserOpenID] {
				g.logger.Printf("[gateway] ignoring c2c message from non-allowlisted user %s", m.Author.UserOpenID)
				return
			}
		} else if g.restricted() {
			// restricted to groups only, with no C2C allowlist → don't serve private DMs
			// (otherwise anyone could DM a full-authority bot). Mirrors the group branch.
			g.logger.Printf("[gateway] ignoring c2c message (restricted; no allowed_users set)")
			return
		}
		r := &responder{
			client: g.client, kind: kindC2C,
			userOpenID: m.Author.UserOpenID, msgID: m.ID,
			asMarkdown: g.cfg.ReplyAsMarkdown,
		}
		g.dispatch(ctx, r, cleanContent(m.Content), m.Attachments)

	case qq.EventAtMessageCreate, qq.EventMessageCreate:
		if g.restricted() {
			g.logger.Printf("[gateway] ignoring guild channel message (restricted mode)")
			return
		}
		var m qq.Message
		if err := json.Unmarshal(p.Data, &m); err != nil {
			g.logger.Printf("[gateway] decode channel message: %v", err)
			return
		}
		r := &responder{
			client: g.client, kind: kindChannel,
			channelID: m.ChannelID, msgID: m.ID,
			asMarkdown: g.cfg.ReplyAsMarkdown,
		}
		g.dispatch(ctx, r, cleanContent(m.Content), m.Attachments)

	case qq.EventDirectMessageCreate:
		if g.restricted() {
			g.logger.Printf("[gateway] ignoring guild direct message (restricted mode)")
			return
		}
		var m qq.Message
		if err := json.Unmarshal(p.Data, &m); err != nil {
			g.logger.Printf("[gateway] decode direct message: %v", err)
			return
		}
		r := &responder{
			client: g.client, kind: kindDM,
			guildID: m.GuildID, msgID: m.ID,
			asMarkdown: g.cfg.ReplyAsMarkdown,
		}
		g.dispatch(ctx, r, cleanContent(m.Content), m.Attachments)

	case qq.EventInteractionCreate:
		g.handleInteraction(ctx, p)

	case qq.EventGroupAddRobot:
		g.logger.Printf("[gateway] bot added to a group")
	case qq.EventFriendAdd:
		g.logger.Printf("[gateway] user added the bot as a friend")
	default:
		// Other events (guild/channel/member lifecycle, reactions, audit, audio)
		// are received and logged; extend here as needed.
		g.logger.Printf("[gateway] event %s received", p.Type)
	}
}

// handleInteraction acknowledges button callbacks so the user gets immediate
// feedback, then treats the button data as a conversational prompt.
func (g *Gateway) handleInteraction(ctx context.Context, p *qq.Payload) {
	var it qq.Interaction
	if err := json.Unmarshal(p.Data, &it); err != nil {
		g.logger.Printf("[gateway] decode interaction: %v", err)
		return
	}
	// Enforce the allowlist on interactions too, so button callbacks can't be
	// used to bypass the restriction.
	if g.restricted() {
		switch it.ChatType {
		case qq.InteractionChatTypeC2C:
			if g.allowedUsers != nil {
				if !g.allowedUsers[it.UserOpenID] {
					g.logger.Printf("[gateway] ignoring interaction from non-allowlisted user %s", it.UserOpenID)
					return
				}
			} else {
				// restricted with no C2C allowlist → ignore C2C button callbacks too
				g.logger.Printf("[gateway] ignoring c2c interaction (restricted; no allowed_users set)")
				return
			}
		case qq.InteractionChatTypeGroup:
			if g.allowedGroups == nil || !g.allowedGroups[it.GroupOpenID] {
				g.logger.Printf("[gateway] ignoring group interaction (not allowlisted)")
				return
			}
		default:
			g.logger.Printf("[gateway] ignoring channel interaction (restricted mode)")
			return
		}
	}
	// Always ACK promptly.
	if err := g.client.AckInteraction(ctx, it.ID, qq.InteractionACKSuccess); err != nil {
		g.logger.Printf("[gateway] ack interaction: %v", err)
	}
	prompt := it.Data.Resolved.ButtonData
	if prompt == "" {
		return
	}
	var r *responder
	switch it.ChatType {
	case qq.InteractionChatTypeGroup:
		r = &responder{client: g.client, kind: kindGroup, groupOpenID: it.GroupOpenID, eventID: it.ID, asMarkdown: g.cfg.ReplyAsMarkdown}
	case qq.InteractionChatTypeC2C:
		r = &responder{client: g.client, kind: kindC2C, userOpenID: it.UserOpenID, eventID: it.ID, asMarkdown: g.cfg.ReplyAsMarkdown}
	default:
		r = &responder{client: g.client, kind: kindChannel, channelID: it.ChannelID, eventID: it.ID, asMarkdown: g.cfg.ReplyAsMarkdown}
	}
	g.dispatch(ctx, r, prompt, nil)
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

	if handled := g.handleCommand(ctx, r, key, text); handled {
		return
	}

	// Detach from the request context so a webhook response (which cancels its
	// context on return) doesn't kill the in-flight turn.
	go g.runTurn(context.Background(), r, key, text, atts)
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
			"\n\n把对应的 open_id 填入配置的 `allowed_users` / `allowed_groups` 即可锁定操作者。")
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
	auth := "❌"
	if u, err := claude.FetchUsage(context.Background()); err == nil {
		auth = "✅"
		if u.Plan != "" {
			plan = prettyPlan(u.Plan)
		}
	}
	txt := fmt.Sprintf(
		"**🩺 环境诊断**\n"+
			"Claude: %s\n"+
			"订阅认证: %s %s\n"+
			"网关: v%s · 运行 %s · 连接正常\n"+
			"权限: %s",
		strings.TrimSpace(ver), auth, plan, Version, g.uptime(), authorityLabel(g.bridge.FullAuthority()))
	_ = r.Send(context.Background(), txt)
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
	var b strings.Builder
	b.WriteString("**📊 订阅用量**")
	if u.Plan != "" {
		b.WriteString(" · " + prettyPlan(u.Plan))
	}
	b.WriteString("\n")
	b.WriteString(fmtWindow("5 小时窗口", u.FiveHour))
	b.WriteString(fmtWindow("7 天窗口", u.SevenDay))
	if u.Opus.Has {
		b.WriteString(fmtWindow("7 天 · Opus", u.Opus))
	}
	if u.Sonnet.Has {
		b.WriteString(fmtWindow("7 天 · Sonnet", u.Sonnet))
	}
	return strings.TrimRight(b.String(), "\n")
}

func fmtWindow(label string, w claude.Window) string {
	if !w.Has {
		return ""
	}
	line := fmt.Sprintf("**%s** 已用 %.0f%%", label, w.Utilization)
	if !w.ResetsAt.IsZero() {
		line += fmt.Sprintf(" · %s后重置（%s）", humanDur(time.Until(w.ResetsAt)), w.ResetsAt.In(cstZone).Format("01-02 15:04"))
	}
	return line + "\n"
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
	return fmt.Sprintf(
		"**📊 运行状态**\n"+
			"**会话** %s\n"+
			"**模型** %s\n"+
			"**目录** %s\n"+
			"**权限** %s\n"+
			"**任务** %s · **轮数** %d\n"+
			"**运行** %s · v%s",
		status, model, workDir, authority, running, s.TurnCount(), g.uptime(), Version)
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
	var b strings.Builder
	fmt.Fprintf(&b, "**💬 活跃会话** %d 个 · 累计 %d 轮 · $%.4f\n", len(snaps), turns, cost)
	for _, s := range snaps {
		state := "空闲"
		if s.Running {
			state = "运行中"
		}
		conn := "新会话"
		if s.Active {
			conn = "已连接"
		}
		idle := time.Since(s.LastActive).Round(time.Second)
		fmt.Fprintf(&b, "· `%s` — %s · %s · %d 轮 · 闲置 %s\n", s.Key, conn, state, s.Turns, idle)
	}
	return strings.TrimRight(b.String(), "\n")
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

const helpText = "**🤖 Claude Code · QQ** —— 直接说需求即可，命令可选：\n\n" +
	"| 命令 | 说明 | 命令 | 说明 |\n" +
	"| --- | --- | --- | --- |\n" +
	"| /new | 新对话 | /agents | 后台子代理 |\n" +
	"| /retry | 重做上一条 | /review | 代码评审 |\n" +
	"| /stop | 中断任务 | /diff | 查看 git 改动 |\n" +
	"| /model | 切换模型 | /mcp | MCP 服务器 |\n" +
	"| /think | 深度思考 | /memory | 查看记忆 |\n" +
	"| /dir | 工作目录 | /explain | 解释代码/内容 |\n" +
	"| /mode | 权限模式 | /web | 联网搜索 |\n" +
	"| /usage | 用量额度 | /doctor | 环境诊断 |\n" +
	"| /cost | 上次花费 | /init | 生成 CLAUDE.md |\n" +
	"| /status | 运行状态 | /sessions | 活跃会话 |\n" +
	"| /whoami | 我的 open_id | /version | 版本信息 |\n" +
	"| /help | 显示帮助 | | |"

// maxPassiveReplies is QQ's cap on passive replies per inbound message.
const maxPassiveReplies = 5

// runTurn executes one Claude Code turn for a conversation and sends the reply,
// including any inbound attachments (downloaded for Claude to read) and any
// outbound media Claude asks to send.
func (g *Gateway) runTurn(ctx context.Context, r *responder, key, text string, atts []qq.MessageAttachment) {
	sess := g.sessions.Get(key)
	sess.Lock()
	defer sess.Unlock()

	// Make the turn cancellable so /stop can interrupt it.
	turnCtx, cancel := context.WithCancel(ctx)
	sess.BeginTurn(cancel)
	defer func() {
		sess.EndTurn()
		cancel()
	}()

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
	})
	if err != nil {
		if turnCtx.Err() == context.Canceled {
			g.logger.Printf("[gateway] [%s] turn cancelled", key)
			return
		}
		// A hard failure (process error / timeout / an unresumable session id). If we
		// were resuming, the stored id may be stale — clear it so the NEXT message
		// starts a fresh conversation instead of re-failing forever on a bad --resume.
		if resuming != "" {
			sess.ClearClaude()
			g.logger.Printf("[gateway] [%s] cleared possibly-stale session id after error", key)
		}
		g.logger.Printf("[gateway] [%s] claude error: %v", key, err)
		_ = r.Send(ctx, "⚠️ 出错了 (Claude error): "+short(err.Error()))
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
		_ = r.Send(ctx, msg)
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
	g.deliver(ctx, r, key, reply)
}

// deliver sends Claude's reply: extracts outbound media directives, sends the
// text (as a file when it is too long to fit the passive-reply budget), then
// delivers each media item — all within QQ's 5-passive-reply cap.
func (g *Gateway) deliver(ctx context.Context, r *responder, key, text string) {
	text, media := extractSendDirectives(text)

	budget := maxPassiveReplies
	chunks := splitMessage(text, g.cfg.MaxReplyChars)
	textBudget := budget - len(media)
	if textBudget < 1 {
		textBudget = 1
	}

	if text != "" && len(chunks) > textBudget {
		// Too long to fit: deliver the full text as a file when we can upload.
		if g.cfg.LongRepliesAsFile() && r.supportsUpload() {
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
				return
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
