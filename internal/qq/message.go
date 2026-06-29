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
	Keyboard *MessageKeyboard `json:"keyboard,omitempty"`
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

// MediaUploadRequest uploads rich media for a C2C message. The gateway always
// uploads then sends in two steps (it never sets srv_send_msg), so only the
// upload inputs are modeled here.
type MediaUploadRequest struct {
	FileType int    `json:"file_type"`
	URL      string `json:"url,omitempty"`
	FileData string `json:"file_data,omitempty"`
}

// MediaUploadResponse is the result of a rich-media upload. Only file_info is
// used — it is passed straight into the follow-up MsgTypeMedia send.
type MediaUploadResponse struct {
	FileInfo string `json:"file_info,omitempty"`
}

// UploadC2CMedia uploads rich media destined for a user (single chat).
func (c *Client) UploadC2CMedia(ctx context.Context, userOpenID string, req *MediaUploadRequest) (*MediaUploadResponse, error) {
	var out MediaUploadResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/users/"+userOpenID+"/files", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
