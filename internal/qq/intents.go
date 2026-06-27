package qq

// Intent is a WebSocket gateway subscription bitmask.
type Intent int

// This single-chat gateway needs exactly one intent: the GROUP_AND_C2C bit that
// delivers C2C (private) messages. (QQ has no C2C-only bit — this same bit also
// carries group messages, which the gateway simply ignores.)
const (
	IntentGroupAndC2CEvent Intent = 1 << 25 // C2C/GROUP messages and management events
)

// intentNames maps the supported intent name (as used in config) to its bit.
var intentNames = map[string]Intent{
	"GROUP_AND_C2C_EVENT": IntentGroupAndC2CEvent,
}

// IntentsFromNames resolves a list of intent names to a combined bitmask.
// Unknown names are ignored; an empty/unknown set falls back to the default.
func IntentsFromNames(names []string) Intent {
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

// DefaultIntents returns the only intent a single-chat gateway needs.
func DefaultIntents() Intent {
	return IntentGroupAndC2CEvent
}
