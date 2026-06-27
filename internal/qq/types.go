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

// MessageAttachment is a rich-media attachment carried by an inbound message.
type MessageAttachment struct {
	ContentType string `json:"content_type,omitempty"`
	Filename    string `json:"filename,omitempty"`
	URL         string `json:"url,omitempty"`
	Size        int    `json:"size,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	ID          string `json:"id,omitempty"`
}

// MessageMarkdown is a native markdown body (msg_type=2).
type MessageMarkdown struct {
	Content string `json:"content,omitempty"`
}

// MessageMedia references uploaded rich media for a C2C send (msg_type=7).
type MessageMedia struct {
	FileInfo string `json:"file_info,omitempty"`
}

// MessageResponse is the minimal body returned by a C2C send. QQ returns the
// timestamp here as an ISO-8601 string (e.g. "2026-06-27T15:45:20+08:00").
type MessageResponse struct {
	ID        string `json:"id,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}
