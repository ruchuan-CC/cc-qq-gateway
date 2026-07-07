package qq

// This file defines the data models used by the single-chat (C2C) gateway.
// All identifiers are JSON strings on the wire (never numbers), per the official
// QQ Bot OpenAPI v2 documentation, so every ID field is typed as string.

// User is the bot account as returned by GET /users/@me and the READY event.
type User struct {
	ID          string `json:"id,omitempty"`
	Username    string `json:"username,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	Bot         bool   `json:"bot,omitempty"`
	UnionOpenID string `json:"union_openid,omitempty"`
}

// MessageMarkdown is a native markdown body (msg_type=2).
type MessageMarkdown struct {
	Content string `json:"content,omitempty"`
}

// MessageAttachment is a rich-media attachment carried by an inbound C2C
// message. The gateway downloads inbound attachments and passes local paths to
// Codex, but does not support outbound media.
type MessageAttachment struct {
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	URL         string `json:"url,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	ID          string `json:"id,omitempty"`
}

// MessageResponse is the minimal body returned by a C2C send. QQ returns the
// timestamp here as an ISO-8601 string (e.g. "2026-06-27T15:45:20+08:00").
type MessageResponse struct {
	ID        string `json:"id,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}
