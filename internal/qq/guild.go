package qq

import (
	"context"
	"net/http"
	"strconv"
)

// ---------------------------------------------------------------------------
// Guild
// ---------------------------------------------------------------------------

// GetGuild returns a guild's detail.
func (c *Client) GetGuild(ctx context.Context, guildID string) (*Guild, error) {
	var out Guild
	err := c.doJSON(ctx, http.MethodGet, "/guilds/"+guildID, nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMe returns the current bot user.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var out User
	err := c.doJSON(ctx, http.MethodGet, "/users/@me", nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMyGuilds returns the guilds the bot belongs to. before/after/limit paginate.
func (c *Client) GetMyGuilds(ctx context.Context, before, after string, limit int) ([]Guild, error) {
	q := map[string]string{"before": before, "after": after}
	if limit > 0 {
		q["limit"] = strconv.Itoa(limit)
	}
	var out []Guild
	err := c.doJSON(ctx, http.MethodGet, "/users/@me/guilds"+query(q), nil, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Channel
// ---------------------------------------------------------------------------

// GetChannels lists a guild's sub-channels.
func (c *Client) GetChannels(ctx context.Context, guildID string) ([]Channel, error) {
	var out []Channel
	err := c.doJSON(ctx, http.MethodGet, "/guilds/"+guildID+"/channels", nil, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetChannel returns a sub-channel's detail.
func (c *Client) GetChannel(ctx context.Context, channelID string) (*Channel, error) {
	var out Channel
	err := c.doJSON(ctx, http.MethodGet, "/channels/"+channelID, nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateChannelRequest creates a sub-channel.
type CreateChannelRequest struct {
	Name            string   `json:"name"`
	Type            int      `json:"type"`
	SubType         int      `json:"sub_type"`
	Position        int      `json:"position"`
	ParentID        string   `json:"parent_id,omitempty"`
	PrivateType     int      `json:"private_type,omitempty"`
	PrivateUserIDs  []string `json:"private_user_ids,omitempty"`
	SpeakPermission int      `json:"speak_permission,omitempty"`
	ApplicationID   string   `json:"application_id,omitempty"`
}

// CreateChannel creates a sub-channel (private-domain admin only).
func (c *Client) CreateChannel(ctx context.Context, guildID string, req *CreateChannelRequest) (*Channel, error) {
	var out Channel
	err := c.doJSON(ctx, http.MethodPost, "/guilds/"+guildID+"/channels", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ModifyChannelRequest patches a sub-channel; send only changed fields.
type ModifyChannelRequest struct {
	Name            *string `json:"name,omitempty"`
	Position        *int    `json:"position,omitempty"`
	ParentID        *string `json:"parent_id,omitempty"`
	PrivateType     *int    `json:"private_type,omitempty"`
	SpeakPermission *int    `json:"speak_permission,omitempty"`
}

// ModifyChannel patches a sub-channel (private-domain only).
func (c *Client) ModifyChannel(ctx context.Context, channelID string, req *ModifyChannelRequest) (*Channel, error) {
	var out Channel
	err := c.doJSON(ctx, http.MethodPatch, "/channels/"+channelID, req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteChannel deletes a sub-channel (private-domain only, irreversible).
func (c *Client) DeleteChannel(ctx context.Context, channelID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/channels/"+channelID, nil, nil)
}

// ---------------------------------------------------------------------------
// Channel permissions
// ---------------------------------------------------------------------------

// GetChannelMemberPermissions returns a member's permissions in a channel.
func (c *Client) GetChannelMemberPermissions(ctx context.Context, channelID, userID string) (*ChannelPermissions, error) {
	var out ChannelPermissions
	err := c.doJSON(ctx, http.MethodGet, "/channels/"+channelID+"/members/"+userID+"/permissions", nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateChannelMemberPermissions adds/removes permission bits for a member.
func (c *Client) UpdateChannelMemberPermissions(ctx context.Context, channelID, userID, add, remove string) error {
	body := map[string]string{"add": add, "remove": remove}
	return c.doJSON(ctx, http.MethodPut, "/channels/"+channelID+"/members/"+userID+"/permissions", body, nil)
}

// GetChannelRolePermissions returns a role's permissions in a channel.
func (c *Client) GetChannelRolePermissions(ctx context.Context, channelID, roleID string) (*ChannelPermissions, error) {
	var out ChannelPermissions
	err := c.doJSON(ctx, http.MethodGet, "/channels/"+channelID+"/roles/"+roleID+"/permissions", nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateChannelRolePermissions adds/removes permission bits for a role.
func (c *Client) UpdateChannelRolePermissions(ctx context.Context, channelID, roleID, add, remove string) error {
	body := map[string]string{"add": add, "remove": remove}
	return c.doJSON(ctx, http.MethodPut, "/channels/"+channelID+"/roles/"+roleID+"/permissions", body, nil)
}

// ---------------------------------------------------------------------------
// Members
// ---------------------------------------------------------------------------

// GetGuildMembers lists guild members (private-domain only). after starts at "0".
func (c *Client) GetGuildMembers(ctx context.Context, guildID, after string, limit int) ([]Member, error) {
	q := map[string]string{"after": after}
	if limit > 0 {
		q["limit"] = strconv.Itoa(limit)
	}
	var out []Member
	err := c.doJSON(ctx, http.MethodGet, "/guilds/"+guildID+"/members"+query(q), nil, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetGuildMember returns a single member's detail.
func (c *Client) GetGuildMember(ctx context.Context, guildID, userID string) (*Member, error) {
	var out Member
	err := c.doJSON(ctx, http.MethodGet, "/guilds/"+guildID+"/members/"+userID, nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteMemberRequest configures a member kick.
type DeleteMemberRequest struct {
	AddBlacklist         bool `json:"add_blacklist,omitempty"`
	DeleteHistoryMsgDays int  `json:"delete_history_msg_days,omitempty"`
}

// DeleteGuildMember kicks a member from a guild (private-domain admin only).
func (c *Client) DeleteGuildMember(ctx context.Context, guildID, userID string, req *DeleteMemberRequest) error {
	return c.doJSON(ctx, http.MethodDelete, "/guilds/"+guildID+"/members/"+userID, req, nil)
}

// ---------------------------------------------------------------------------
// Roles
// ---------------------------------------------------------------------------

// GuildRoles is the wrapper returned by GET /guilds/{id}/roles.
type GuildRoles struct {
	GuildID      string `json:"guild_id,omitempty"`
	Roles        []Role `json:"roles,omitempty"`
	RoleNumLimit string `json:"role_num_limit,omitempty"`
}

// GetGuildRoles lists a guild's roles.
func (c *Client) GetGuildRoles(ctx context.Context, guildID string) (*GuildRoles, error) {
	var out GuildRoles
	err := c.doJSON(ctx, http.MethodGet, "/guilds/"+guildID+"/roles", nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// RoleRequest is the body for creating/modifying a role.
type RoleRequest struct {
	Name  string `json:"name,omitempty"`
	Color uint32 `json:"color,omitempty"`
	Hoist int32  `json:"hoist,omitempty"`
}

// RoleResult is returned by create/modify role.
type RoleResult struct {
	RoleID  string `json:"role_id,omitempty"`
	GuildID string `json:"guild_id,omitempty"`
	Role    *Role  `json:"role,omitempty"`
}

// CreateGuildRole creates a role.
func (c *Client) CreateGuildRole(ctx context.Context, guildID string, req *RoleRequest) (*RoleResult, error) {
	var out RoleResult
	err := c.doJSON(ctx, http.MethodPost, "/guilds/"+guildID+"/roles", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ModifyGuildRole patches a role.
func (c *Client) ModifyGuildRole(ctx context.Context, guildID, roleID string, req *RoleRequest) (*RoleResult, error) {
	var out RoleResult
	err := c.doJSON(ctx, http.MethodPatch, "/guilds/"+guildID+"/roles/"+roleID, req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteGuildRole deletes a role.
func (c *Client) DeleteGuildRole(ctx context.Context, guildID, roleID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/guilds/"+guildID+"/roles/"+roleID, nil, nil)
}

// roleMemberBody carries the optional channel id required for role 5 (channel admin).
type roleMemberBody struct {
	Channel *struct {
		ID string `json:"id"`
	} `json:"channel,omitempty"`
}

func roleMemberPayload(channelID string) *roleMemberBody {
	if channelID == "" {
		return nil
	}
	b := &roleMemberBody{}
	b.Channel = &struct {
		ID string `json:"id"`
	}{ID: channelID}
	return b
}

// AddGuildRoleMember adds a member to a role. channelID is required only for
// role id 5 (sub-channel admin); pass "" otherwise.
func (c *Client) AddGuildRoleMember(ctx context.Context, guildID, userID, roleID, channelID string) error {
	return c.doJSON(ctx, http.MethodPut, "/guilds/"+guildID+"/members/"+userID+"/roles/"+roleID, roleMemberPayload(channelID), nil)
}

// RemoveGuildRoleMember removes a member from a role.
func (c *Client) RemoveGuildRoleMember(ctx context.Context, guildID, userID, roleID, channelID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/guilds/"+guildID+"/members/"+userID+"/roles/"+roleID, roleMemberPayload(channelID), nil)
}

// ---------------------------------------------------------------------------
// Mute
// ---------------------------------------------------------------------------

// MuteRequest carries mute timing (string seconds; "0" unmutes).
type MuteRequest struct {
	MuteEndTimestamp string `json:"mute_end_timestamp,omitempty"`
	MuteSeconds      string `json:"mute_seconds,omitempty"`
}

// MuteGuild mutes (or unmutes) everyone in a guild.
func (c *Client) MuteGuild(ctx context.Context, guildID string, req *MuteRequest) error {
	return c.doJSON(ctx, http.MethodPatch, "/guilds/"+guildID+"/mute", req, nil)
}

// MuteGuildMember mutes (or unmutes) one member.
func (c *Client) MuteGuildMember(ctx context.Context, guildID, userID string, req *MuteRequest) error {
	return c.doJSON(ctx, http.MethodPatch, "/guilds/"+guildID+"/members/"+userID+"/mute", req, nil)
}

// MuteMembersRequest mutes a list of members.
type MuteMembersRequest struct {
	MuteEndTimestamp string   `json:"mute_end_timestamp,omitempty"`
	MuteSeconds      string   `json:"mute_seconds,omitempty"`
	UserIDs          []string `json:"user_ids"`
}

// MuteMembersResult lists the members successfully muted.
type MuteMembersResult struct {
	UserIDs []string `json:"user_ids,omitempty"`
}

// MuteGuildMembers mutes multiple members at once.
func (c *Client) MuteGuildMembers(ctx context.Context, guildID string, req *MuteMembersRequest) (*MuteMembersResult, error) {
	var out MuteMembersResult
	err := c.doJSON(ctx, http.MethodPatch, "/guilds/"+guildID+"/mute", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Announcements
// ---------------------------------------------------------------------------

// CreateAnnounceRequest creates a guild announcement.
type CreateAnnounceRequest struct {
	MessageID         string             `json:"message_id,omitempty"`
	ChannelID         string             `json:"channel_id,omitempty"`
	AnnouncesType     uint32             `json:"announces_type,omitempty"`
	RecommendChannels []RecommendChannel `json:"recommend_channels,omitempty"`
}

// CreateGuildAnnounce sets a guild announcement.
func (c *Client) CreateGuildAnnounce(ctx context.Context, guildID string, req *CreateAnnounceRequest) (*Announces, error) {
	var out Announces
	err := c.doJSON(ctx, http.MethodPost, "/guilds/"+guildID+"/announces", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteGuildAnnounce removes a guild announcement. messageID "all" clears.
func (c *Client) DeleteGuildAnnounce(ctx context.Context, guildID, messageID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/guilds/"+guildID+"/announces/"+messageID, nil, nil)
}

// CreateChannelAnnounce sets a sub-channel announcement (deprecated; use pins).
func (c *Client) CreateChannelAnnounce(ctx context.Context, channelID, messageID string) (*Announces, error) {
	body := map[string]string{"message_id": messageID}
	var out Announces
	err := c.doJSON(ctx, http.MethodPost, "/channels/"+channelID+"/announces", body, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteChannelAnnounce removes a sub-channel announcement (deprecated).
func (c *Client) DeleteChannelAnnounce(ctx context.Context, channelID, messageID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/channels/"+channelID+"/announces/"+messageID, nil, nil)
}

// ---------------------------------------------------------------------------
// Pins (精华)
// ---------------------------------------------------------------------------

// AddPin pins a message (max 20 per channel).
func (c *Client) AddPin(ctx context.Context, channelID, messageID string) (*PinsMessage, error) {
	var out PinsMessage
	err := c.doJSON(ctx, http.MethodPut, "/channels/"+channelID+"/pins/"+messageID, nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePin unpins a message. messageID "all" clears all pins.
func (c *Client) DeletePin(ctx context.Context, channelID, messageID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/channels/"+channelID+"/pins/"+messageID, nil, nil)
}

// GetPins lists a channel's pinned messages.
func (c *Client) GetPins(ctx context.Context, channelID string) (*PinsMessage, error) {
	var out PinsMessage
	err := c.doJSON(ctx, http.MethodGet, "/channels/"+channelID+"/pins", nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Schedules
// ---------------------------------------------------------------------------

// GetSchedules lists a channel's schedules. since is ms epoch (0 = today).
func (c *Client) GetSchedules(ctx context.Context, channelID string, since uint64) ([]Schedule, error) {
	q := map[string]string{}
	if since > 0 {
		q["since"] = strconv.FormatUint(since, 10)
	}
	var out []Schedule
	err := c.doJSON(ctx, http.MethodGet, "/channels/"+channelID+"/schedules"+query(q), nil, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetSchedule returns one schedule.
func (c *Client) GetSchedule(ctx context.Context, channelID, scheduleID string) (*Schedule, error) {
	var out Schedule
	err := c.doJSON(ctx, http.MethodGet, "/channels/"+channelID+"/schedules/"+scheduleID, nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type scheduleBody struct {
	Schedule *Schedule `json:"schedule"`
}

// CreateSchedule creates a schedule.
func (c *Client) CreateSchedule(ctx context.Context, channelID string, s *Schedule) (*Schedule, error) {
	var out Schedule
	err := c.doJSON(ctx, http.MethodPost, "/channels/"+channelID+"/schedules", &scheduleBody{Schedule: s}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ModifySchedule updates a schedule.
func (c *Client) ModifySchedule(ctx context.Context, channelID, scheduleID string, s *Schedule) (*Schedule, error) {
	var out Schedule
	err := c.doJSON(ctx, http.MethodPatch, "/channels/"+channelID+"/schedules/"+scheduleID, &scheduleBody{Schedule: s}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSchedule deletes a schedule.
func (c *Client) DeleteSchedule(ctx context.Context, channelID, scheduleID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/channels/"+channelID+"/schedules/"+scheduleID, nil, nil)
}

// ---------------------------------------------------------------------------
// Reactions
// ---------------------------------------------------------------------------

// AddReaction adds the bot's reaction to a message. emojiType is 1 (system) or
// 2 (unicode); emojiID is the numeric id or the unicode character.
func (c *Client) AddReaction(ctx context.Context, channelID, messageID string, emojiType int, emojiID string) error {
	return c.doJSON(ctx, http.MethodPut,
		"/channels/"+channelID+"/messages/"+messageID+"/reactions/"+strconv.Itoa(emojiType)+"/"+emojiID, nil, nil)
}

// RemoveReaction removes the bot's reaction from a message.
func (c *Client) RemoveReaction(ctx context.Context, channelID, messageID string, emojiType int, emojiID string) error {
	return c.doJSON(ctx, http.MethodDelete,
		"/channels/"+channelID+"/messages/"+messageID+"/reactions/"+strconv.Itoa(emojiType)+"/"+emojiID, nil, nil)
}

// GetReactionUsers lists the users who reacted with an emoji. cookie paginates.
func (c *Client) GetReactionUsers(ctx context.Context, channelID, messageID string, emojiType int, emojiID, cookie string, limit int) (*ReactionUsers, error) {
	q := map[string]string{"cookie": cookie}
	if limit > 0 {
		q["limit"] = strconv.Itoa(limit)
	}
	var out ReactionUsers
	err := c.doJSON(ctx, http.MethodGet,
		"/channels/"+channelID+"/messages/"+messageID+"/reactions/"+strconv.Itoa(emojiType)+"/"+emojiID+query(q), nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Audio / Mic
// ---------------------------------------------------------------------------

// ControlAudio controls audio playback in a voice channel.
func (c *Client) ControlAudio(ctx context.Context, channelID string, ctrl *AudioControl) error {
	return c.doJSON(ctx, http.MethodPost, "/channels/"+channelID+"/audio", ctrl, nil)
}

// MicOn puts the bot on mic in an audio channel.
func (c *Client) MicOn(ctx context.Context, channelID string) error {
	return c.doJSON(ctx, http.MethodPut, "/channels/"+channelID+"/mic", map[string]any{}, nil)
}

// MicOff takes the bot off mic.
func (c *Client) MicOff(ctx context.Context, channelID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/channels/"+channelID+"/mic", map[string]any{}, nil)
}

// ---------------------------------------------------------------------------
// API permissions
// ---------------------------------------------------------------------------

// GetAPIPermissions lists the APIs the bot may call in a guild.
func (c *Client) GetAPIPermissions(ctx context.Context, guildID string) (*APIPermissions, error) {
	var out APIPermissions
	err := c.doJSON(ctx, http.MethodGet, "/guilds/"+guildID+"/api_permission", nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DemandAPIPermissionRequest requests a permission-grant link.
type DemandAPIPermissionRequest struct {
	ChannelID   string                       `json:"channel_id"`
	APIIdentify *APIPermissionDemandIdentify `json:"api_identify"`
	Desc        string                       `json:"desc"`
}

// DemandAPIPermission posts an authorization-link message to a channel.
func (c *Client) DemandAPIPermission(ctx context.Context, guildID string, req *DemandAPIPermissionRequest) (*APIPermissionDemand, error) {
	var out APIPermissionDemand
	err := c.doJSON(ctx, http.MethodPost, "/guilds/"+guildID+"/api_permission/demand", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Interactions
// ---------------------------------------------------------------------------

// Interaction ACK codes.
const (
	InteractionACKSuccess      = 0
	InteractionACKFailed       = 1
	InteractionACKTooFrequent  = 2
	InteractionACKDuplicate    = 3
	InteractionACKNoPermission = 4
	InteractionACKAdminOnly    = 5
)

// AckInteraction acknowledges a button/menu interaction, controlling the toast
// shown to the user.
func (c *Client) AckInteraction(ctx context.Context, interactionID string, code int) error {
	body := map[string]int{"code": code}
	return c.doJSON(ctx, http.MethodPut, "/interactions/"+interactionID, body, nil)
}

// ---------------------------------------------------------------------------
// Gateway
// ---------------------------------------------------------------------------

// SessionStartLimit describes WebSocket session creation quotas.
type SessionStartLimit struct {
	Total          int `json:"total"`
	Remaining      int `json:"remaining"`
	ResetAfter     int `json:"reset_after"`
	MaxConcurrency int `json:"max_concurrency"`
}

// GatewayInfo is the result of GET /gateway/bot.
type GatewayInfo struct {
	URL               string            `json:"url"`
	Shards            int               `json:"shards"`
	SessionStartLimit SessionStartLimit `json:"session_start_limit"`
}

// GetGateway returns the general WSS gateway URL.
func (c *Client) GetGateway(ctx context.Context) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/gateway", nil, &out)
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

// GetGatewayBot returns the sharded WSS gateway info (recommended).
func (c *Client) GetGatewayBot(ctx context.Context) (*GatewayInfo, error) {
	var out GatewayInfo
	err := c.doJSON(ctx, http.MethodGet, "/gateway/bot", nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ColorToARGBDecimal converts an RGB hex (e.g. 0xRRGGBB) plus alpha (0-255) to
// the decimal ARGB value the role color field expects.
func ColorToARGBDecimal(r, g, b, a uint8) uint32 {
	return uint32(a)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}
