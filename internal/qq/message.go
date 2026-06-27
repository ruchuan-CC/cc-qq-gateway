package qq

import (
	"context"
	"net/http"
)

// msg_type values for the C2C send endpoint.
const (
	MsgTypeText     = 0
	MsgTypeMarkdown = 2
	MsgTypeMedia    = 7
)

// MessageRequest is the body for a C2C send (POST /v2/users/{openid}/messages).
type MessageRequest struct {
	Content  string           `json:"content,omitempty"`
	MsgType  int              `json:"msg_type"`
	Markdown *MessageMarkdown `json:"markdown,omitempty"`
	Media    *MessageMedia    `json:"media,omitempty"`
	EventID  string           `json:"event_id,omitempty"`
	MsgID    string           `json:"msg_id,omitempty"`
	MsgSeq   int              `json:"msg_seq,omitempty"`
}

// SendC2CMessage sends a message to a user in single chat.
func (c *Client) SendC2CMessage(ctx context.Context, userOpenID string, req *MessageRequest) (*MessageResponse, error) {
	var out MessageResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+userOpenID+"/messages", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Rich media upload (C2C)
// ---------------------------------------------------------------------------

// Rich media file types.
const (
	FileTypeImage = 1
	FileTypeVideo = 2
	FileTypeAudio = 3
	FileTypeFile  = 4
)

// MediaUploadRequest uploads rich media for a C2C message.
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

// UploadC2CMedia uploads rich media destined for a user (single chat).
func (c *Client) UploadC2CMedia(ctx context.Context, userOpenID string, req *MediaUploadRequest) (*MediaUploadResponse, error) {
	var out MediaUploadResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+userOpenID+"/files", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
