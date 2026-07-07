package qq

import (
	"context"
	"net/http"
)

// msg_type values for the C2C send endpoint.
const (
	MsgTypeText     = 0
	MsgTypeMarkdown = 2
)

// MessageRequest is the body for a C2C send (POST /v2/users/{openid}/messages).
type MessageRequest struct {
	Content  string           `json:"content,omitempty"`
	MsgType  int              `json:"msg_type"`
	Markdown *MessageMarkdown `json:"markdown,omitempty"`
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
