// Package gateway wires QQ C2C text messages to a local Codex CLI session and
// routes Codex replies back to the same QQ user.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"runtime/debug"
	"strings"

	"github.com/chenhg5/cc-qq-gateway/internal/codex"
	"github.com/chenhg5/cc-qq-gateway/internal/config"
	"github.com/chenhg5/cc-qq-gateway/internal/qq"
	"github.com/chenhg5/cc-qq-gateway/internal/session"
)

// Version is the gateway build version.
const Version = "0.7.0"

type qqSender interface {
	SendC2CMessage(context.Context, string, *qq.MessageRequest) (*qq.MessageResponse, error)
}

type codexRunner interface {
	Run(context.Context, codex.Request) (*codex.Result, error)
}

// Gateway is the central text proxy.
type Gateway struct {
	client   qqSender
	bridge   codexRunner
	sessions *session.Manager
	cfg      config.GatewayConfig
	logger   *log.Logger

	allowedUsers map[string]bool
}

// New builds a Gateway.
func New(client qqSender, bridge codexRunner, sessions *session.Manager, cfg config.GatewayConfig, logger *log.Logger) *Gateway {
	if logger == nil {
		logger = log.Default()
	}
	return &Gateway{
		client:       client,
		bridge:       bridge,
		sessions:     sessions,
		cfg:          cfg,
		logger:       logger,
		allowedUsers: toSet(cfg.AllowedUsers),
	}
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

// HandleEvent is the qq.EventHandler. This gateway handles only C2C messages;
// lifecycle events are logged and every other QQ surface is ignored.
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
		g.dispatch(ctx, r, m.ID, cleanContent(m.Content), m.Attachments)

	case qq.EventFriendAdd, qq.EventFriendDel, qq.EventC2CMsgReject, qq.EventC2CMsgReceive:
		g.logFriendEvent(p)
	case qq.EventReady, qq.EventResumed:
		return
	default:
		g.logger.Printf("[gateway] ignoring non-C2C event %s", p.Type)
	}
}

func (g *Gateway) logFriendEvent(p *qq.Payload) {
	var ev qq.C2CManageEvent
	if err := json.Unmarshal(p.Data, &ev); err != nil {
		g.logger.Printf("[gateway] decode %s event: %v", p.Type, err)
		return
	}
	g.logger.Printf("[gateway] %s user open_id=%s", p.Type, ev.User())
}

func (g *Gateway) dispatch(ctx context.Context, r *responder, msgID, text string, atts []qq.MessageAttachment) {
	if strings.TrimSpace(text) == "" && len(atts) == 0 {
		return
	}
	key := r.conversationKey()
	g.logger.Printf("[gateway] inbound %s len=%d attachments=%d", key, len([]rune(text)), len(atts))

	g.flushPending(ctx, r, key)
	if strings.TrimSpace(text) == "" {
		g.safeGo("attachments "+key, func() {
			refs := g.materializeAttachments(context.Background(), key, msgID, atts)
			g.sessions.Get(key).QueuePendingAttachments(refs)
			_ = r.Send(context.Background(), "附件已保存，请补一句要处理什么。")
		})
		return
	}
	g.safeGo("turn "+key, func() {
		g.runTurn(context.Background(), r, key, msgID, text, atts)
	})
}

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

func (g *Gateway) flushPending(ctx context.Context, r *responder, key string) {
	pending := g.sessions.Get(key).TakePending()
	if len(pending) == 0 {
		return
	}
	for _, p := range pending {
		g.deliverOrQueue(ctx, r, g.sessions.Get(key), key, p)
	}
}

func (g *Gateway) runTurn(ctx context.Context, r *responder, key, msgID, text string, atts []qq.MessageAttachment) {
	sess := g.sessions.Get(key)
	defer func() {
		if rec := recover(); rec != nil {
			g.logger.Printf("[gateway] [%s] turn PANIC: %v\n%s", key, rec, debug.Stack())
			g.deliverOrQueue(context.Background(), r, sess, key, "internal gateway error")
		}
	}()

	sess.Lock()
	defer sess.Unlock()
	sess.BeginTurn()
	defer sess.EndTurn()

	resuming := sess.GetSessionID()
	refs := sess.TakePendingAttachments()
	refs = append(refs, g.materializeAttachments(ctx, key, msgID, atts)...)
	prompt := composePrompt(text, refs)
	g.logger.Printf("[gateway] [%s] running codex turn resume=%t", key, resuming != "")
	res, err := g.bridge.Run(ctx, codex.Request{
		SessionID: resuming,
		Prompt:    prompt,
		OnActivity: func(tool string) {
			g.logger.Printf("[gateway] [%s] tool: %s", key, tool)
		},
	})
	if err != nil {
		if res != nil && res.SessionID != "" {
			sess.SetSessionID(res.SessionID)
		} else if resuming != "" {
			sess.ClearThread()
		}
		g.persist()
		g.logger.Printf("[gateway] [%s] codex error: %v", key, err)
		g.deliverOrQueue(ctx, r, sess, key, err.Error())
		return
	}
	if res.IsError {
		msg := strings.TrimSpace(res.Text)
		if msg == "" {
			msg = "Codex returned an error"
		}
		g.logger.Printf("[gateway] [%s] codex returned is_error: %s", key, short(msg))
		g.deliverOrQueue(ctx, r, sess, key, msg)
		return
	}
	if res.SessionID != "" {
		sess.SetSessionID(res.SessionID)
	}
	sess.IncTurn()
	g.persist()

	reply := strings.TrimSpace(res.Text)
	if reply == "" {
		reply = "(empty response)"
	}
	g.deliverOrQueue(ctx, r, sess, key, reply)
}

const maxPassiveReplies = 5

var errPassiveBudget = errors.New("passive-reply budget exhausted for this message")

func (g *Gateway) deliver(ctx context.Context, r *responder, text string) (remaining string, err error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	budget := maxPassiveReplies
	if !r.Active() {
		budget = maxPassiveReplies - r.SentCount()
		if budget < 1 {
			return text, errPassiveBudget
		}
	}

	chunks := splitMessage(text, g.cfg.MaxReplyChars)
	for i, c := range chunks {
		if i >= budget {
			return strings.Join(chunks[i:], "\n"), nil
		}
		if err := r.Send(ctx, c); err != nil {
			return strings.Join(chunks[i:], "\n"), err
		}
	}
	return "", nil
}

// deliverOrQueue tries the current passive reply window first, then one active
// push, and finally queues any unsent text for the user's next inbound message.
func (g *Gateway) deliverOrQueue(ctx context.Context, r *responder, sess *session.Session, key, text string) {
	remaining, err := g.deliver(ctx, r, text)
	if err == nil && remaining == "" {
		return
	}
	if err != nil {
		g.logger.Printf("[gateway] [%s] passive delivery failed (%v); trying active push", key, err)
		if remaining == "" {
			remaining = text
		}
		r.GoActive()
		remaining, err = g.deliver(ctx, r, remaining)
	}
	if err != nil || remaining != "" {
		if remaining == "" {
			remaining = text
		}
		sess.QueuePending(remaining)
		g.persist()
		g.logger.Printf("[gateway] [%s] queued undelivered reply text", key)
	}
}

func (g *Gateway) persist() {
	if err := g.sessions.SaveState(); err != nil {
		g.logger.Printf("[gateway] save state: %v", err)
	}
}

// SaveState persists sessions on shutdown.
func (g *Gateway) SaveState() {
	g.persist()
}
