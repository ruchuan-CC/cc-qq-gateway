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

// responder sends replies back to the single-chat (C2C) user a message came from.
// It tracks the passive-reply sequence number required by the C2C send API.
type responder struct {
	client *qq.Client

	userOpenID string
	msgID      string // inbound message id, for passive replies

	seq int // passive-reply sequence

	asMarkdown bool
}

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
func (r *responder) Send(ctx context.Context, text string) error {
	useMarkdown := r.asMarkdown && !markdownDisabled.Load()
	err := r.sendOnce(ctx, text, useMarkdown)
	if err != nil && useMarkdown {
		// Always retry as plain text so the message is still delivered. Only disable
		// markdown PROCESS-WIDE when the API itself rejected it (a 4xx APIError — e.g.
		// passive markdown not approved); a transient network/5xx error must NOT
		// permanently downgrade every future reply to plain text.
		var apiErr *qq.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
			log.Printf("[gateway] markdown rejected by API (%v); falling back to plain text for the process", err)
			markdownDisabled.Store(true)
		} else {
			log.Printf("[gateway] markdown send failed transiently (%v); retrying as text (markdown stays enabled)", err)
		}
		return r.sendOnce(ctx, text, false)
	}
	return err
}

// sendOnce performs exactly one C2C send in the requested format.
func (r *responder) sendOnce(ctx context.Context, text string, asMarkdown bool) error {
	r.seq++
	req := &qq.MessageRequest{MsgID: r.msgID, MsgSeq: r.seq}
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
	r.seq++
	req := &qq.MessageRequest{
		MsgType: qq.MsgTypeMedia,
		Media:   &qq.MessageMedia{FileInfo: info.FileInfo},
		MsgID:   r.msgID, MsgSeq: r.seq,
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
