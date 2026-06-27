package qq

// Intent is a WebSocket gateway subscription bitmask. Combine with bitwise OR.
type Intent int

// Intent values, per the official api-v2 event-emit documentation.
const (
	IntentGuilds                Intent = 1 << 0  // GUILD_CREATE/UPDATE/DELETE, CHANNEL_CREATE/UPDATE/DELETE
	IntentGuildMembers          Intent = 1 << 1  // GUILD_MEMBER_ADD/UPDATE/REMOVE
	IntentGuildMessages         Intent = 1 << 9  // MESSAGE_CREATE/DELETE (private-domain bots)
	IntentGuildMessageReactions Intent = 1 << 10 // MESSAGE_REACTION_ADD/REMOVE
	IntentDirectMessage         Intent = 1 << 12 // DIRECT_MESSAGE_CREATE/DELETE
	IntentOpenForumsEvent       Intent = 1 << 18 // OPEN_FORUM_* (public-domain, legacy/SDK)
	IntentAudioOrLiveMember     Intent = 1 << 19 // AUDIO_OR_LIVE_CHANNEL_MEMBER_* (legacy/SDK)
	IntentGroupAndC2CEvent      Intent = 1 << 25 // C2C/GROUP messages and management events
	IntentInteraction           Intent = 1 << 26 // INTERACTION_CREATE
	IntentMessageAudit          Intent = 1 << 27 // MESSAGE_AUDIT_PASS/REJECT
	IntentForumsEvent           Intent = 1 << 28 // FORUM_* (private-domain)
	IntentAudioAction           Intent = 1 << 29 // AUDIO_START/FINISH/ON_MIC/OFF_MIC
	IntentPublicGuildMessages   Intent = 1 << 30 // AT_MESSAGE_CREATE, PUBLIC_MESSAGE_DELETE
)

// intentNames maps known intent names (as used in config) to their bit values.
var intentNames = map[string]Intent{
	"GUILDS":                  IntentGuilds,
	"GUILD_MEMBERS":           IntentGuildMembers,
	"GUILD_MESSAGES":          IntentGuildMessages,
	"GUILD_MESSAGE_REACTIONS": IntentGuildMessageReactions,
	"DIRECT_MESSAGE":          IntentDirectMessage,
	"OPEN_FORUMS_EVENT":       IntentOpenForumsEvent,
	"AUDIO_OR_LIVE_MEMBER":    IntentAudioOrLiveMember,
	"GROUP_AND_C2C_EVENT":     IntentGroupAndC2CEvent,
	"INTERACTION":             IntentInteraction,
	"MESSAGE_AUDIT":           IntentMessageAudit,
	"FORUMS_EVENT":            IntentForumsEvent,
	"AUDIO_ACTION":            IntentAudioAction,
	"PUBLIC_GUILD_MESSAGES":   IntentPublicGuildMessages,
}

// IntentsFromNames resolves a list of intent names to a combined bitmask.
// Unknown names are ignored. If names is empty, a sensible default for a
// conversational bot is returned (group/C2C + public guild + direct + interaction).
func IntentsFromNames(names []string) Intent {
	if len(names) == 0 {
		return DefaultIntents()
	}
	var out Intent
	for _, n := range names {
		if v, ok := intentNames[n]; ok {
			out |= v
		}
	}
	if out == 0 {
		return DefaultIntents()
	}
	return out
}

// DefaultIntents returns the intents a conversational gateway needs: group/C2C
// messages, public guild @-mentions, guild direct messages, and button callbacks.
func DefaultIntents() Intent {
	return IntentGroupAndC2CEvent |
		IntentPublicGuildMessages |
		IntentDirectMessage |
		IntentGuilds |
		IntentInteraction
}
