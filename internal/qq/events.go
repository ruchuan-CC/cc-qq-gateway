package qq

import "encoding/json"

// Event type (the "t" field of an op-0 dispatch / the webhook event type).
//
// This gateway is single-chat (C2C / private) only: it subscribes to and handles
// just the C2C message event. Lifecycle events (READY/RESUMED) are handled by the
// WebSocket transport; every other QQ surface (group, guild channel, guild DM,
// interaction, reactions, audio, audit) was intentionally removed.
const (
	// Lifecycle (WebSocket only)
	EventReady   = "READY"
	EventResumed = "RESUMED"

	// The only conversational event we handle.
	EventC2CMessageCreate = "C2C_MESSAGE_CREATE"
)

// Payload is the unified gateway/webhook envelope.
type Payload struct {
	ID   string          `json:"id,omitempty"` // present on webhook pushes
	Op   int             `json:"op"`
	Data json.RawMessage `json:"d,omitempty"`
	Seq  int64           `json:"s,omitempty"` // sequence (op 0 only)
	Type string          `json:"t,omitempty"` // event name (op 0 only)
}

// WebSocket opcodes.
const (
	OpDispatch           = 0
	OpHeartbeat          = 1
	OpIdentify           = 2
	OpResume             = 6
	OpReconnect          = 7
	OpInvalidSession     = 9
	OpHello              = 10
	OpHeartbeatACK       = 11
	OpHTTPCallbackACK    = 12
	OpCallbackValidation = 13
)

// HelloData is op 10.
type HelloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

// ReadyData is the READY event.
type ReadyData struct {
	Version   int    `json:"version"`
	SessionID string `json:"session_id"`
	User      User   `json:"user"`
	Shard     []int  `json:"shard"`
}

// C2CMessageAuthor identifies the sender of a C2C (single-chat) message.
type C2CMessageAuthor struct {
	UserOpenID  string `json:"user_openid,omitempty"`
	UnionOpenID string `json:"union_openid,omitempty"`
}

// C2CMessage is the C2C_MESSAGE_CREATE payload (a single-chat message).
type C2CMessage struct {
	ID          string              `json:"id,omitempty"`
	Content     string              `json:"content,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
	Author      C2CMessageAuthor    `json:"author,omitempty"`
	Attachments []MessageAttachment `json:"attachments,omitempty"`
}

// CallbackValidation is the op-13 webhook validation payload.
type CallbackValidation struct {
	PlainToken string `json:"plain_token"`
	EventTs    string `json:"event_ts"`
}

// CallbackValidationResponse is the op-13 response body.
type CallbackValidationResponse struct {
	PlainToken string `json:"plain_token"`
	Signature  string `json:"signature"`
}
