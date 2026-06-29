package gateway

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"sync/atomic"

	"github.com/chenhg5/cc-qq-gateway/internal/qq"
)

// markdownDisabled is set the first time the API rejects a markdown send (e.g.
// the bot is not approved for native markdown), after which replies fall back to
// plain text for the rest of the process. Zero value = markdown enabled.
var markdownDisabled atomic.Bool

// qqErrMsgDeduped is the QQ C2C error code "消息被去重，请检查请求msgseq": the
// msg_seq collided with an earlier send. It is about sequencing, not markdown, so
// it must never latch the process-wide markdown downgrade.
const qqErrMsgDeduped = 40054005

// disablesMarkdown reports whether a failed markdown send indicates the bot is not
// permitted to use markdown at all (a stable condition worth latching), rather than
// an unrelated 4xx that merely happened to surface on a markdown send.
func disablesMarkdown(e *qq.APIError) bool {
	switch e.Code {
	case qqErrMsgDeduped:
		return false
	}
	return true
}

// responder sends replies back to the single-chat (C2C) user a message came from.
// The QQ msg_seq required by the C2C send API is drawn from nextSeq, a
// per-conversation monotonic counter (see session.Session.NextSeq) so that
// consecutive turns, active pushes and notify messages to the same user never
// reuse a seq — reuse is rejected by QQ as code 40054005.
type responder struct {
	client *qq.Client

	userOpenID string
	msgID      string // inbound message id, for passive replies
	eventID    string // event id, for passive replies to events (e.g. FRIEND_ADD) that carry no msg_id

	// nextSeq yields the next monotonic msg_seq for this conversation. Always set
	// by the gateway when building a responder (bound to the session's counter).
	nextSeq func() int

	// active, once set, switches sends from passive replies (bound to msgID, which
	// QQ expires after ~5 minutes) to active pushes (msgID omitted). Used to deliver
	// the result of a turn that outran the passive-reply window.
	active atomic.Bool

	asMarkdown bool
}

// GoActive switches this responder to active-push mode for all subsequent sends.
func (r *responder) GoActive() { r.active.Store(true) }

// Active reports whether the responder is in active-push mode.
func (r *responder) Active() bool { return r.active.Load() }

// conversationKey returns the stable per-conversation key for session tracking.
func (r *responder) conversationKey() string {
	return "c2c:" + r.userOpenID
}

// identity returns a human-readable description of the origin, used by /whoami
// (handy for filling in allowed_users).
func (r *responder) identity() string {
	return "私聊 (C2C) user open_id=" + r.userOpenID
}

// Send delivers a single message chunk to the user. When markdown is requested it
// is attempted first; if the API rejects it the chunk is retried as plain text.
// A final failure is logged with the QQ error code: many callers fire-and-forget
// (`_ = r.Send(...)`), so without this a rejected reply (rate limit, expired
// passive window, permission) would vanish with no trace.
func (r *responder) Send(ctx context.Context, text string) error {
	useMarkdown := r.asMarkdown && !markdownDisabled.Load()
	err := r.sendOnce(ctx, text, useMarkdown)
	if err != nil && useMarkdown {
		// Always retry as plain text so the message is still delivered. Only disable
		// markdown PROCESS-WIDE when the API rejected markdown ITSELF (the bot is not
		// approved for native markdown — a stable condition worth latching). An
		// unrelated 4xx that merely surfaced on a markdown send (e.g. msgseq dedup
		// 40054005, rate limiting) or a transient network/5xx error must NOT
		// permanently downgrade every future reply to plain text.
		var apiErr *qq.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 && disablesMarkdown(apiErr) {
			log.Printf("[gateway] markdown rejected by API (%v); falling back to plain text for the process", err)
			markdownDisabled.Store(true)
		} else {
			log.Printf("[gateway] markdown send failed (%v); retrying as text (markdown stays enabled)", err)
		}
		err = r.sendOnce(ctx, text, false)
	}
	if err != nil {
		log.Printf("[gateway] C2C send FAILED (active=%t) to open_id=%s: %v", r.active.Load(), r.userOpenID, err)
	}
	return err
}

// sendOnce performs exactly one C2C send in the requested format.
func (r *responder) sendOnce(ctx context.Context, text string, asMarkdown bool) error {
	req := &qq.MessageRequest{MsgSeq: r.nextSeq()}
	if !r.active.Load() {
		// Passive reply: bind to the inbound msg_id, or an event_id when the reply
		// answers an event (e.g. FRIEND_ADD) that carries no message. Active pushes
		// omit both.
		req.MsgID = r.msgID
		if r.msgID == "" {
			req.EventID = r.eventID
		}
	}
	applyContent(req, text, asMarkdown)
	_, err := r.client.SendC2CMessage(ctx, r.userOpenID, req)
	return err
}

// SendMedia uploads one media item (image/file/video/audio) and sends it to the
// user. localPath is preferred when non-empty, else the URL is uploaded by ref.
func (r *responder) SendMedia(ctx context.Context, fileType int, localPath, url string) error {
	up := &qq.MediaUploadRequest{FileType: fileType}
	if url != "" {
		up.URL = url
	} else {
		data, err := os.ReadFile(localPath)
		if err != nil {
			return fmt.Errorf("read media %s: %w", localPath, err)
		}
		up.FileData = base64.StdEncoding.EncodeToString(data)
	}
	info, err := r.client.UploadC2CMedia(ctx, r.userOpenID, up)
	if err != nil {
		return fmt.Errorf("upload media: %w", err)
	}
	req := &qq.MessageRequest{
		MsgType: qq.MsgTypeMedia,
		Media:   &qq.MessageMedia{FileInfo: info.FileInfo},
		MsgSeq:  r.nextSeq(),
	}
	if !r.active.Load() {
		req.MsgID = r.msgID
	}
	_, err = r.client.SendC2CMessage(ctx, r.userOpenID, req)
	return err
}

// applyContent fills a C2C request as text or markdown.
func applyContent(req *qq.MessageRequest, text string, asMarkdown bool) {
	if asMarkdown {
		req.MsgType = qq.MsgTypeMarkdown
		req.Markdown = &qq.MessageMarkdown{Content: text}
		return
	}
	req.MsgType = qq.MsgTypeText
	req.Content = text
}
