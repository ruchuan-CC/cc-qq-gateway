package qq

import (
	"context"
	"net/http"
)

// msg_type values for the group/C2C send endpoints.
const (
	MsgTypeText     = 0
	MsgTypeMarkdown = 2
	MsgTypeArk      = 3
	MsgTypeEmbed    = 4
	MsgTypeMedia    = 7
)

// MessageRequest is the body for group/C2C sends (POST /v2/groups|users/.../messages).
type MessageRequest struct {
	Content          string            `json:"content,omitempty"`
	MsgType          int               `json:"msg_type"`
	Markdown         *MessageMarkdown  `json:"markdown,omitempty"`
	Keyboard         *MessageKeyboard  `json:"keyboard,omitempty"`
	Media            *MessageMedia     `json:"media,omitempty"`
	Ark              *MessageArk       `json:"ark,omitempty"`
	Embed            *MessageEmbed     `json:"embed,omitempty"`
	MessageReference *MessageReference `json:"message_reference,omitempty"`
	EventID          string            `json:"event_id,omitempty"`
	MsgID            string            `json:"msg_id,omitempty"`
	MsgSeq           int               `json:"msg_seq,omitempty"`
	IsWakeup         bool              `json:"is_wakeup,omitempty"` // C2C only
}

// ChannelMessageRequest is the body for channel/DM sends.
type ChannelMessageRequest struct {
	Content          string            `json:"content,omitempty"`
	Embed            *MessageEmbed     `json:"embed,omitempty"`
	Ark              *MessageArk       `json:"ark,omitempty"`
	MessageReference *MessageReference `json:"message_reference,omitempty"`
	Image            string            `json:"image,omitempty"`
	Markdown         *MessageMarkdown  `json:"markdown,omitempty"`
	Keyboard         *MessageKeyboard  `json:"keyboard,omitempty"`
	MsgID            string            `json:"msg_id,omitempty"`
	EventID          string            `json:"event_id,omitempty"`
}

// SendGroupMessage sends a message to a group.
func (c *Client) SendGroupMessage(ctx context.Context, groupOpenID string, req *MessageRequest) (*MessageResponse, error) {
	var out MessageResponse
	err := c.doJSON(ctx, http.MethodPost, "/v2/groups/"+groupOpenID+"/messages", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SendC2CMessage sends a message to a user in single chat.
func (c *Client) SendC2CMessage(ctx context.Context, userOpenID string, req *MessageRequest) (*MessageResponse, error) {
	var out MessageResponse
	err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+userOpenID+"/messages", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SendChannelMessage sends a message to a guild text sub-channel.
func (c *Client) SendChannelMessage(ctx context.Context, channelID string, req *ChannelMessageRequest) (*Message, error) {
	var out Message
	err := c.doJSON(ctx, http.MethodPost, "/channels/"+channelID+"/messages", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDMS creates a direct-message session with a user.
func (c *Client) CreateDMS(ctx context.Context, recipientID, sourceGuildID string) (*DMS, error) {
	body := map[string]string{"recipient_id": recipientID, "source_guild_id": sourceGuildID}
	var out DMS
	err := c.doJSON(ctx, http.MethodPost, "/users/@me/dms", body, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SendDirectMessage sends a guild direct message. guildID is the DMS.GuildID
// returned by CreateDMS.
func (c *Client) SendDirectMessage(ctx context.Context, guildID string, req *ChannelMessageRequest) (*Message, error) {
	var out Message
	err := c.doJSON(ctx, http.MethodPost, "/dms/"+guildID+"/messages", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Rich media upload (group/C2C)
// ---------------------------------------------------------------------------

// Rich media file types.
const (
	FileTypeImage = 1
	FileTypeVideo = 2
	FileTypeAudio = 3
	FileTypeFile  = 4
)

// MediaUploadRequest uploads rich media for a group or C2C message.
type MediaUploadRequest struct {
	FileType   int    `json:"file_type"`
	URL        string `json:"url,omitempty"`
	SrvSendMsg bool   `json:"srv_send_msg"`
	FileData   string `json:"file_data,omitempty"`
}

// MediaUploadResponse is the result of a rich-media upload.
type MediaUploadResponse struct {
	FileUUID string `json:"file_uuid,omitempty"`
	FileInfo string `json:"file_info,omitempty"`
	TTL      int    `json:"ttl,omitempty"`
	ID       string `json:"id,omitempty"` // present only when SrvSendMsg=true
}

// UploadGroupMedia uploads rich media destined for a group.
func (c *Client) UploadGroupMedia(ctx context.Context, groupOpenID string, req *MediaUploadRequest) (*MediaUploadResponse, error) {
	var out MediaUploadResponse
	err := c.doJSON(ctx, http.MethodPost, "/v2/groups/"+groupOpenID+"/files", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UploadC2CMedia uploads rich media destined for a user (single chat).
func (c *Client) UploadC2CMedia(ctx context.Context, userOpenID string, req *MediaUploadRequest) (*MediaUploadResponse, error) {
	var out MediaUploadResponse
	err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+userOpenID+"/files", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Recall (withdraw) messages
// ---------------------------------------------------------------------------

// RecallChannelMessage withdraws a channel message.
func (c *Client) RecallChannelMessage(ctx context.Context, channelID, messageID string, hidetip bool) error {
	q := map[string]string{}
	if hidetip {
		q["hidetip"] = "true"
	}
	return c.doJSON(ctx, http.MethodDelete, "/channels/"+channelID+"/messages/"+messageID+query(q), nil, nil)
}

// RecallDirectMessage withdraws a direct message.
func (c *Client) RecallDirectMessage(ctx context.Context, guildID, messageID string, hidetip bool) error {
	q := map[string]string{}
	if hidetip {
		q["hidetip"] = "true"
	}
	return c.doJSON(ctx, http.MethodDelete, "/dms/"+guildID+"/messages/"+messageID+query(q), nil, nil)
}

// RecallGroupMessage withdraws a group message.
func (c *Client) RecallGroupMessage(ctx context.Context, groupOpenID, messageID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/v2/groups/"+groupOpenID+"/messages/"+messageID, nil, nil)
}

// RecallC2CMessage withdraws a C2C message.
func (c *Client) RecallC2CMessage(ctx context.Context, userOpenID, messageID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/v2/users/"+userOpenID+"/messages/"+messageID, nil, nil)
}
