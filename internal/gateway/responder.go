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

// markdownDisabled is set the first time a markdown send is rejected (e.g. the
// bot is not approved for native markdown), after which replies fall back to
// plain text for the rest of the process. Zero value = markdown enabled.
var markdownDisabled atomic.Bool

// chatKind identifies the conversation surface.
type chatKind string

const (
	kindGroup   chatKind = "group"
	kindC2C     chatKind = "c2c"
	kindChannel chatKind = "channel"
	kindDM      chatKind = "dm"
)

// responder knows how to send replies back to the origin of a message. It
// tracks the passive-reply sequence number required by the group/C2C APIs.
type responder struct {
	client *qq.Client
	kind   chatKind

	groupOpenID string
	userOpenID  string
	channelID   string
	guildID     string // DMS guild id for direct messages
	msgID       string // inbound message id used for passive replies
	eventID     string // inbound event id (used when there is no msgID)

	seq int // passive reply sequence (group/C2C)

	asMarkdown bool
}

// conversationKey returns the stable per-conversation key for session tracking.
func (r *responder) conversationKey() string {
	switch r.kind {
	case kindGroup:
		return "group:" + r.groupOpenID
	case kindC2C:
		return "c2c:" + r.userOpenID
	case kindChannel:
		return "channel:" + r.channelID
	case kindDM:
		return "dm:" + r.guildID
	}
	return "unknown"
}

// identity returns a human-readable description of the origin ids, used by the
// /whoami command (handy for filling in allowed_groups / allowed_users).
func (r *responder) identity() string {
	switch r.kind {
	case kindGroup:
		return "群聊 (group) open_id=" + r.groupOpenID
	case kindC2C:
		return "私聊 (C2C) user open_id=" + r.userOpenID
	case kindChannel:
		return "子频道 (channel) channel_id=" + r.channelID
	case kindDM:
		return "频道私信 (guild DM) guild_id=" + r.guildID
	}
	return "unknown"
}

// Send delivers a single message chunk to the conversation origin. When markdown
// is requested it is attempted first; if the send is rejected (e.g. the bot is
// not approved for native markdown) it disables markdown process-wide and retries
// the same chunk as plain text, so replies are always delivered.
func (r *responder) Send(ctx context.Context, text string) error {
	useMarkdown := r.asMarkdown && !markdownDisabled.Load()
	err := r.sendOnce(ctx, text, useMarkdown)
	if err != nil && useMarkdown {
		// Always retry as plain text so the message is still delivered. Only disable
		// markdown PROCESS-WIDE when the API itself rejected it (a 4xx APIError — e.g.
		// passive markdown not approved); a transient network/5xx error must NOT
		// permanently downgrade every future conversation to plain text.
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

// sendOnce performs exactly one send in the requested format.
func (r *responder) sendOnce(ctx context.Context, text string, asMarkdown bool) error {
	switch r.kind {
	case kindGroup:
		r.seq++
		req := &qq.MessageRequest{MsgID: r.msgID, EventID: r.eventID, MsgSeq: r.seq}
		applyContent(&req.MsgType, &req.Content, &req.Markdown, text, asMarkdown)
		_, err := r.client.SendGroupMessage(ctx, r.groupOpenID, req)
		return err
	case kindC2C:
		r.seq++
		req := &qq.MessageRequest{MsgID: r.msgID, EventID: r.eventID, MsgSeq: r.seq}
		applyContent(&req.MsgType, &req.Content, &req.Markdown, text, asMarkdown)
		_, err := r.client.SendC2CMessage(ctx, r.userOpenID, req)
		return err
	case kindChannel:
		req := &qq.ChannelMessageRequest{MsgID: r.msgID, EventID: r.eventID}
		applyChannelContent(req, text, asMarkdown)
		_, err := r.client.SendChannelMessage(ctx, r.channelID, req)
		return err
	case kindDM:
		req := &qq.ChannelMessageRequest{MsgID: r.msgID, EventID: r.eventID}
		applyChannelContent(req, text, asMarkdown)
		_, err := r.client.SendDirectMessage(ctx, r.guildID, req)
		return err
	}
	return nil
}

// supportsUpload reports whether this surface can deliver uploaded rich media
// (group/C2C). Channels/DMs only support image-by-URL.
func (r *responder) supportsUpload() bool {
	return r.kind == kindGroup || r.kind == kindC2C
}

// SendMedia delivers one media item (image/file/video/audio). For group/C2C it
// uploads the bytes (or passes a URL) and sends a media message; for channel/DM
// it can only embed an image URL. localPath is preferred when non-empty.
func (r *responder) SendMedia(ctx context.Context, fileType int, localPath, url string) error {
	switch r.kind {
	case kindGroup, kindC2C:
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
		var info *qq.MediaUploadResponse
		var err error
		if r.kind == kindGroup {
			info, err = r.client.UploadGroupMedia(ctx, r.groupOpenID, up)
		} else {
			info, err = r.client.UploadC2CMedia(ctx, r.userOpenID, up)
		}
		if err != nil {
			return fmt.Errorf("upload media: %w", err)
		}
		r.seq++
		req := &qq.MessageRequest{
			MsgType: qq.MsgTypeMedia,
			Media:   &qq.MessageMedia{FileInfo: info.FileInfo},
			MsgID:   r.msgID, EventID: r.eventID, MsgSeq: r.seq,
		}
		if r.kind == kindGroup {
			_, err = r.client.SendGroupMessage(ctx, r.groupOpenID, req)
		} else {
			_, err = r.client.SendC2CMessage(ctx, r.userOpenID, req)
		}
		return err
	case kindChannel, kindDM:
		if fileType != qq.FileTypeImage || url == "" {
			return fmt.Errorf("channel/DM media supports image-by-URL only")
		}
		req := &qq.ChannelMessageRequest{Image: url, MsgID: r.msgID, EventID: r.eventID}
		if r.kind == kindChannel {
			_, err := r.client.SendChannelMessage(ctx, r.channelID, req)
			return err
		}
		_, err := r.client.SendDirectMessage(ctx, r.guildID, req)
		return err
	}
	return fmt.Errorf("unknown surface")
}

// applyContent fills a group/C2C request as text or markdown.
func applyContent(msgType *int, content *string, md **qq.MessageMarkdown, text string, asMarkdown bool) {
	if asMarkdown {
		*msgType = qq.MsgTypeMarkdown
		*md = &qq.MessageMarkdown{Content: text}
		return
	}
	*msgType = qq.MsgTypeText
	*content = text
}

// applyChannelContent fills a channel/DM request as text or markdown.
func applyChannelContent(req *qq.ChannelMessageRequest, text string, asMarkdown bool) {
	if asMarkdown {
		req.Markdown = &qq.MessageMarkdown{Content: text}
		return
	}
	req.Content = text
}
