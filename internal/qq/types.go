package qq

// This file defines the data models used across the QQ Bot OpenAPI v2.
// All identifiers are JSON strings on the wire (never numbers), per the
// official documentation, so every ID field is typed as string.

// ---------------------------------------------------------------------------
// Common objects
// ---------------------------------------------------------------------------

// User is a QQ user/bot account as returned by guild-domain APIs and events.
type User struct {
	ID               string `json:"id,omitempty"`
	Username         string `json:"username,omitempty"`
	Avatar           string `json:"avatar,omitempty"`
	Bot              bool   `json:"bot,omitempty"`
	UnionOpenID      string `json:"union_openid,omitempty"`
	UnionUserAccount string `json:"union_user_account,omitempty"`
}

// Member is a guild member.
type Member struct {
	User     *User    `json:"user,omitempty"`
	Nick     string   `json:"nick,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	JoinedAt string   `json:"joined_at,omitempty"`
	Deaf     bool     `json:"deaf,omitempty"`
	Mute     bool     `json:"mute,omitempty"`
	Pending  bool     `json:"pending,omitempty"`
}

// MemberWithGuildID is the payload of guild member events.
type MemberWithGuildID struct {
	GuildID  string   `json:"guild_id,omitempty"`
	User     *User    `json:"user,omitempty"`
	Nick     string   `json:"nick,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	JoinedAt string   `json:"joined_at,omitempty"`
	OpUserID string   `json:"op_user_id,omitempty"`
}

// Guild is a QQ channel-guild (频道).
type Guild struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Icon        string `json:"icon,omitempty"`
	OwnerID     string `json:"owner_id,omitempty"`
	Owner       bool   `json:"owner,omitempty"`
	JoinedAt    string `json:"joined_at,omitempty"`
	MemberCount int    `json:"member_count,omitempty"`
	MaxMembers  int    `json:"max_members,omitempty"`
	Description string `json:"description,omitempty"`
	OpUserID    string `json:"op_user_id,omitempty"` // present on guild events
}

// Channel type / sub-type / privacy enums.
const (
	ChannelTypeText        = 0
	ChannelTypeVoice       = 2
	ChannelTypeCategory    = 4
	ChannelTypeLive        = 10005
	ChannelTypeApplication = 10006
	ChannelTypeForum       = 10007

	ChannelSubTypeChat         = 0
	ChannelSubTypeAnnouncement = 1
	ChannelSubTypeStrategy     = 2
	ChannelSubTypeTeam         = 3

	PrivateTypePublic        = 0
	PrivateTypeAdminOnly     = 1
	PrivateTypeAdminAndUsers = 2

	SpeakPermissionInvalid       = 0
	SpeakPermissionEveryone      = 1
	SpeakPermissionAdminAndUsers = 2
)

// Channel is a sub-channel (子频道).
type Channel struct {
	ID              string `json:"id,omitempty"`
	GuildID         string `json:"guild_id,omitempty"`
	Name            string `json:"name,omitempty"`
	Type            int    `json:"type"`
	SubType         int    `json:"sub_type"`
	Position        int    `json:"position,omitempty"`
	ParentID        string `json:"parent_id,omitempty"`
	OwnerID         string `json:"owner_id,omitempty"`
	PrivateType     int    `json:"private_type,omitempty"`
	SpeakPermission int    `json:"speak_permission,omitempty"`
	ApplicationID   string `json:"application_id,omitempty"`
	Permissions     string `json:"permissions,omitempty"`
	OpUserID        string `json:"op_user_id,omitempty"` // present on channel events
}

// Role is a guild identity-group (身份组).
type Role struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Color       uint32 `json:"color,omitempty"`
	Hoist       int32  `json:"hoist,omitempty"`
	Number      int32  `json:"number,omitempty"`
	MemberLimit int32  `json:"member_limit,omitempty"`
}

// Built-in role IDs.
const (
	RoleIDAll          = "1"
	RoleIDAdmin        = "2"
	RoleIDOwner        = "4"
	RoleIDChannelAdmin = "5"
)

// ---------------------------------------------------------------------------
// Message objects
// ---------------------------------------------------------------------------

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

// MessageReference quotes another message (channel/DM domain).
type MessageReference struct {
	MessageID             string `json:"message_id"`
	IgnoreGetMessageError bool   `json:"ignore_get_message_error,omitempty"`
}

// MessageEmbedThumbnail is the small image of an embed.
type MessageEmbedThumbnail struct {
	URL string `json:"url,omitempty"`
}

// MessageEmbedField is one line of an embed.
type MessageEmbedField struct {
	Name string `json:"name,omitempty"`
}

// MessageEmbed is an embed message (channel/DM, and group/C2C via msg_type=4).
type MessageEmbed struct {
	Title     string                 `json:"title,omitempty"`
	Prompt    string                 `json:"prompt,omitempty"`
	Thumbnail *MessageEmbedThumbnail `json:"thumbnail,omitempty"`
	Fields    []MessageEmbedField    `json:"fields,omitempty"`
}

// MessageArkObjKv / MessageArkObj / MessageArkKv / MessageArk model ark templates.
type MessageArkObjKv struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

type MessageArkObj struct {
	ObjKv []MessageArkObjKv `json:"obj_kv,omitempty"`
}

type MessageArkKv struct {
	Key   string          `json:"key,omitempty"`
	Value string          `json:"value,omitempty"`
	Obj   []MessageArkObj `json:"obj,omitempty"`
}

type MessageArk struct {
	TemplateID int            `json:"template_id,omitempty"`
	Kv         []MessageArkKv `json:"kv,omitempty"`
}

// MarkdownParams fills a template-markdown placeholder.
type MarkdownParams struct {
	Key    string   `json:"key,omitempty"`
	Values []string `json:"values,omitempty"`
}

// MessageMarkdown is a native or template markdown body.
type MessageMarkdown struct {
	Content          string           `json:"content,omitempty"`
	CustomTemplateID string           `json:"custom_template_id,omitempty"`
	Params           []MarkdownParams `json:"params,omitempty"`
}

// MessageMedia references uploaded rich media for group/C2C sends (msg_type=7).
type MessageMedia struct {
	FileInfo string `json:"file_info,omitempty"`
}

// ---------------------------------------------------------------------------
// Inline keyboard (buttons)
// ---------------------------------------------------------------------------

// Keyboard action types.
const (
	ButtonActionJump     = 0 // jump to a URL / mini-program
	ButtonActionCallback = 1 // callback to bot backend (INTERACTION_CREATE)
	ButtonActionCommand  = 2 // @bot and auto-fill a command into the input box

	ButtonStyleGrey = 0
	ButtonStyleBlue = 1

	PermissionTypeSpecifyUsers = 0
	PermissionTypeAdmin        = 1
	PermissionTypeEveryone     = 2
	PermissionTypeSpecifyRoles = 3
)

type Permission struct {
	Type           int      `json:"type"`
	SpecifyUserIDs []string `json:"specify_user_ids,omitempty"`
	SpecifyRoleIDs []string `json:"specify_role_ids,omitempty"`
}

type RenderData struct {
	Label        string `json:"label,omitempty"`
	VisitedLabel string `json:"visited_label,omitempty"`
	Style        int    `json:"style"`
}

type Action struct {
	Type                 int         `json:"type"`
	Permission           *Permission `json:"permission,omitempty"`
	Data                 string      `json:"data,omitempty"`
	Reply                bool        `json:"reply,omitempty"`
	Enter                bool        `json:"enter,omitempty"`
	Anchor               int         `json:"anchor,omitempty"`
	UnsupportTips        string      `json:"unsupport_tips,omitempty"`
	ClickLimit           int         `json:"click_limit,omitempty"`
	AtBotShowChannelList bool        `json:"at_bot_show_channel_list,omitempty"`
}

type Button struct {
	ID         string      `json:"id,omitempty"`
	RenderData *RenderData `json:"render_data,omitempty"`
	Action     *Action     `json:"action,omitempty"`
}

type InlineKeyboardRow struct {
	Buttons []Button `json:"buttons,omitempty"`
}

type InlineKeyboard struct {
	Rows []InlineKeyboardRow `json:"rows,omitempty"`
}

// MessageKeyboard attaches a template id or a custom keyboard to a markdown message.
type MessageKeyboard struct {
	ID      string          `json:"id,omitempty"`
	Content *InlineKeyboard `json:"content,omitempty"`
}

// Message is the full message object returned by channel/DM sends and events.
type Message struct {
	ID               string              `json:"id,omitempty"`
	ChannelID        string              `json:"channel_id,omitempty"`
	GuildID          string              `json:"guild_id,omitempty"`
	Content          string              `json:"content,omitempty"`
	Timestamp        string              `json:"timestamp,omitempty"`
	EditedTimestamp  string              `json:"edited_timestamp,omitempty"`
	MentionEveryone  bool                `json:"mention_everyone,omitempty"`
	Author           *User               `json:"author,omitempty"`
	Member           *Member             `json:"member,omitempty"`
	Mentions         []User              `json:"mentions,omitempty"`
	Attachments      []MessageAttachment `json:"attachments,omitempty"`
	Embeds           []MessageEmbed      `json:"embeds,omitempty"`
	Ark              *MessageArk         `json:"ark,omitempty"`
	Seq              int                 `json:"seq,omitempty"`
	SeqInChannel     string              `json:"seq_in_channel,omitempty"`
	MessageReference *MessageReference   `json:"message_reference,omitempty"`
	SrcGuildID       string              `json:"src_guild_id,omitempty"`
}

// MessageResponse is the minimal body returned by group/C2C sends. Note QQ
// returns timestamp here as an ISO-8601 string (e.g. "2026-06-27T15:45:20+08:00"),
// not a Unix integer.
type MessageResponse struct {
	ID        string `json:"id,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// ---------------------------------------------------------------------------
// Other server objects
// ---------------------------------------------------------------------------

// DMS is a direct-message session.
type DMS struct {
	GuildID    string `json:"guild_id,omitempty"`
	ChannelID  string `json:"channel_id,omitempty"`
	CreateTime string `json:"create_time,omitempty"`
}

// ChannelPermissions describes a member's or role's permission bitmask in a channel.
type ChannelPermissions struct {
	ChannelID   string `json:"channel_id,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	RoleID      string `json:"role_id,omitempty"`
	Permissions string `json:"permissions,omitempty"`
}

// Channel permission bits.
const (
	ChannelPermissionView   = "1"
	ChannelPermissionManage = "2"
	ChannelPermissionSpeak  = "4"
)

// Schedule remind types.
const (
	RemindTypeNone    = "0"
	RemindTypeAtStart = "1"
	RemindType5Min    = "2"
	RemindType15Min   = "3"
	RemindType30Min   = "4"
	RemindType60Min   = "5"
)

// Schedule is a channel schedule (日程).
type Schedule struct {
	ID             string  `json:"id,omitempty"`
	Name           string  `json:"name,omitempty"`
	Description    string  `json:"description,omitempty"`
	StartTimestamp string  `json:"start_timestamp,omitempty"`
	EndTimestamp   string  `json:"end_timestamp,omitempty"`
	Creator        *Member `json:"creator,omitempty"`
	JumpChannelID  string  `json:"jump_channel_id,omitempty"`
	RemindType     string  `json:"remind_type,omitempty"`
}

// RecommendChannel is a recommended channel inside a guild announcement.
type RecommendChannel struct {
	ChannelID string `json:"channel_id,omitempty"`
	Introduce string `json:"introduce,omitempty"`
}

// Announces is a guild/channel announcement.
type Announces struct {
	GuildID           string             `json:"guild_id,omitempty"`
	ChannelID         string             `json:"channel_id,omitempty"`
	MessageID         string             `json:"message_id,omitempty"`
	AnnouncesType     uint32             `json:"announces_type,omitempty"`
	RecommendChannels []RecommendChannel `json:"recommend_channels,omitempty"`
}

// PinsMessage lists the pinned (精华) messages of a channel.
type PinsMessage struct {
	GuildID    string   `json:"guild_id,omitempty"`
	ChannelID  string   `json:"channel_id,omitempty"`
	MessageIDs []string `json:"message_ids,omitempty"`
}

// Emoji types.
const (
	EmojiTypeSystem  = 1
	EmojiTypeUnicode = 2
)

// Emoji models a reaction emoji.
type Emoji struct {
	ID   string `json:"id,omitempty"`
	Type uint32 `json:"type,omitempty"`
}

// Reaction target types (event only).
const (
	ReactionTargetMessage = 0
	ReactionTargetPost    = 1
	ReactionTargetComment = 2
	ReactionTargetReply   = 3
)

// ReactionTarget is the object a reaction is attached to.
type ReactionTarget struct {
	ID   string `json:"id,omitempty"`
	Type int    `json:"type,omitempty"`
}

// MessageReaction is the payload of reaction events.
type MessageReaction struct {
	UserID    string         `json:"user_id,omitempty"`
	GuildID   string         `json:"guild_id,omitempty"`
	ChannelID string         `json:"channel_id,omitempty"`
	Target    ReactionTarget `json:"target,omitempty"`
	Emoji     Emoji          `json:"emoji,omitempty"`
}

// ReactionUsers is the paginated result of listing reaction users.
type ReactionUsers struct {
	Users  []User `json:"users,omitempty"`
	Cookie string `json:"cookie,omitempty"`
	IsEnd  bool   `json:"is_end,omitempty"`
}

// Audio control status.
const (
	AudioStatusStart  = 0
	AudioStatusPause  = 1
	AudioStatusResume = 2
	AudioStatusStop   = 3
)

// AudioControl controls audio playback in a voice channel.
type AudioControl struct {
	AudioURL string `json:"audio_url,omitempty"`
	Text     string `json:"text,omitempty"`
	Status   int    `json:"status"`
}

// APIPermission describes one API the bot may or may not be authorized to call.
type APIPermission struct {
	Path       string `json:"path,omitempty"`
	Method     string `json:"method,omitempty"`
	Desc       string `json:"desc,omitempty"`
	AuthStatus int    `json:"auth_status,omitempty"`
}

// APIPermissions wraps the list returned by GET .../api_permission.
type APIPermissions struct {
	APIs []APIPermission `json:"apis,omitempty"`
}

// APIPermissionDemandIdentify identifies an API in a permission demand.
type APIPermissionDemandIdentify struct {
	Path   string `json:"path,omitempty"`
	Method string `json:"method,omitempty"`
}

// APIPermissionDemand is the result of creating a permission-demand link.
type APIPermissionDemand struct {
	GuildID     string                       `json:"guild_id,omitempty"`
	ChannelID   string                       `json:"channel_id,omitempty"`
	APIIdentify *APIPermissionDemandIdentify `json:"api_identify,omitempty"`
	Title       string                       `json:"title,omitempty"`
	Desc        string                       `json:"desc,omitempty"`
}

// MessageAudited is the payload of message audit events.
type MessageAudited struct {
	AuditID      string `json:"audit_id,omitempty"`
	MessageID    string `json:"message_id,omitempty"`
	GuildID      string `json:"guild_id,omitempty"`
	ChannelID    string `json:"channel_id,omitempty"`
	AuditTime    string `json:"audit_time,omitempty"`
	CreateTime   string `json:"create_time,omitempty"`
	SeqInChannel string `json:"seq_in_channel,omitempty"`
}
