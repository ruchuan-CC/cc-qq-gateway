package qq

import "encoding/json"

// Event type (the "t" field of an op-0 dispatch / the webhook event type).
const (
	// Lifecycle (WebSocket only)
	EventReady   = "READY"
	EventResumed = "RESUMED"

	// Group & C2C messages and management
	EventGroupAtMessageCreate = "GROUP_AT_MESSAGE_CREATE"
	EventC2CMessageCreate     = "C2C_MESSAGE_CREATE"
	EventGroupAddRobot        = "GROUP_ADD_ROBOT"
	EventGroupDelRobot        = "GROUP_DEL_ROBOT"
	EventGroupMsgReject       = "GROUP_MSG_REJECT"
	EventGroupMsgReceive      = "GROUP_MSG_RECEIVE"
	EventFriendAdd            = "FRIEND_ADD"
	EventFriendDel            = "FRIEND_DEL"
	EventC2CMsgReject         = "C2C_MSG_REJECT"
	EventC2CMsgReceive        = "C2C_MSG_RECEIVE"

	// Channel (guild) messages
	EventAtMessageCreate     = "AT_MESSAGE_CREATE"
	EventPublicMessageDelete = "PUBLIC_MESSAGE_DELETE"
	EventMessageCreate       = "MESSAGE_CREATE"
	EventMessageDelete       = "MESSAGE_DELETE"
	EventDirectMessageCreate = "DIRECT_MESSAGE_CREATE"
	EventDirectMessageDelete = "DIRECT_MESSAGE_DELETE"

	// Guild & channel lifecycle
	EventGuildCreate   = "GUILD_CREATE"
	EventGuildUpdate   = "GUILD_UPDATE"
	EventGuildDelete   = "GUILD_DELETE"
	EventChannelCreate = "CHANNEL_CREATE"
	EventChannelUpdate = "CHANNEL_UPDATE"
	EventChannelDelete = "CHANNEL_DELETE"

	// Guild members
	EventGuildMemberAdd    = "GUILD_MEMBER_ADD"
	EventGuildMemberUpdate = "GUILD_MEMBER_UPDATE"
	EventGuildMemberRemove = "GUILD_MEMBER_REMOVE"

	// Reactions
	EventMessageReactionAdd    = "MESSAGE_REACTION_ADD"
	EventMessageReactionRemove = "MESSAGE_REACTION_REMOVE"

	// Interaction
	EventInteractionCreate = "INTERACTION_CREATE"

	// Message audit
	EventMessageAuditPass   = "MESSAGE_AUDIT_PASS"
	EventMessageAuditReject = "MESSAGE_AUDIT_REJECT"

	// Audio
	EventAudioStart  = "AUDIO_START"
	EventAudioFinish = "AUDIO_FINISH"
	EventAudioOnMic  = "AUDIO_ON_MIC"
	EventAudioOffMic = "AUDIO_OFF_MIC"
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

// ---------------------------------------------------------------------------
// Event payload types
// ---------------------------------------------------------------------------

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

// GroupMessageAuthor identifies the sender of a group message.
type GroupMessageAuthor struct {
	MemberOpenID string `json:"member_openid,omitempty"`
	UnionOpenID  string `json:"union_openid,omitempty"`
}

// C2CMessageAuthor identifies the sender of a C2C message.
type C2CMessageAuthor struct {
	UserOpenID  string `json:"user_openid,omitempty"`
	UnionOpenID string `json:"union_openid,omitempty"`
}

// GroupAtMessage is the GROUP_AT_MESSAGE_CREATE payload.
type GroupAtMessage struct {
	ID          string              `json:"id,omitempty"`
	Content     string              `json:"content,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
	GroupOpenID string              `json:"group_openid,omitempty"`
	Author      GroupMessageAuthor  `json:"author,omitempty"`
	Attachments []MessageAttachment `json:"attachments,omitempty"`
}

// C2CMessage is the C2C_MESSAGE_CREATE payload.
type C2CMessage struct {
	ID          string              `json:"id,omitempty"`
	Content     string              `json:"content,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
	Author      C2CMessageAuthor    `json:"author,omitempty"`
	Attachments []MessageAttachment `json:"attachments,omitempty"`
}

// GroupManageEvent is the payload of GROUP_ADD_ROBOT/DEL_ROBOT/MSG_REJECT/MSG_RECEIVE.
type GroupManageEvent struct {
	GroupOpenID    string `json:"group_openid,omitempty"`
	OpMemberOpenID string `json:"op_member_openid,omitempty"`
	Timestamp      int64  `json:"timestamp,omitempty"`
}

// FriendEvent is the payload of FRIEND_ADD/DEL and C2C_MSG_REJECT/RECEIVE.
type FriendEvent struct {
	OpenID     string `json:"openid,omitempty"`
	Timestamp  int64  `json:"timestamp,omitempty"`
	Scene      int    `json:"scene,omitempty"`
	SceneParam string `json:"scene_param,omitempty"`
}

// MessageDeleteEvent is the payload of *_MESSAGE_DELETE.
type MessageDeleteEvent struct {
	Message Message `json:"message,omitempty"`
	OpUser  User    `json:"op_user,omitempty"`
}

// Interaction chat types.
const (
	InteractionChatTypeGuild = 0
	InteractionChatTypeGroup = 1
	InteractionChatTypeC2C   = 2
)

// InteractionResolved holds the resolved button data.
type InteractionResolved struct {
	ButtonID   string `json:"button_id,omitempty"`
	ButtonData string `json:"button_data,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	MessageID  string `json:"message_id,omitempty"`
	FeatureID  string `json:"feature_id,omitempty"`
}

// InteractionData is the data field of an interaction.
type InteractionData struct {
	Type     int                 `json:"type,omitempty"`
	Resolved InteractionResolved `json:"resolved,omitempty"`
}

// Interaction is the INTERACTION_CREATE payload.
type Interaction struct {
	ID                string          `json:"id,omitempty"`
	ApplicationID     string          `json:"application_id,omitempty"`
	Type              int             `json:"type,omitempty"`
	Scene             string          `json:"scene,omitempty"`
	ChatType          int             `json:"chat_type,omitempty"`
	Version           int             `json:"version,omitempty"`
	Timestamp         string          `json:"timestamp,omitempty"`
	GuildID           string          `json:"guild_id,omitempty"`
	ChannelID         string          `json:"channel_id,omitempty"`
	UserOpenID        string          `json:"user_openid,omitempty"`
	GroupOpenID       string          `json:"group_openid,omitempty"`
	GroupMemberOpenID string          `json:"group_member_openid,omitempty"`
	Data              InteractionData `json:"data,omitempty"`
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
