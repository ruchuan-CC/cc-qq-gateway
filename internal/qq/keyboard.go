package qq

// Inline message buttons (keyboard). Per the QQ v2 docs, custom keyboards are
// open to all bots in single-chat (C2C) and group scenarios since 2026-04-23 with
// no template application required. A keyboard must ride on a markdown message
// (markdown content is mandatory; a keyboard-only message is not allowed). Layout
// limit: at most 5 rows, 5 buttons per row.

// Button action types.
const (
	ButtonActionURL      = 0 // open a link / jump
	ButtonActionCallback = 1 // server callback (INTERACTION_CREATE)
	ButtonActionCommand  = 2 // insert/auto-send the command text into the chat
)

// Button permission types.
const (
	ButtonPermAll = 2 // everyone may click
)

// Button render styles.
const (
	ButtonStyleGray = 0
	ButtonStyleBlue = 1
)

// MessageKeyboard is the keyboard field of a message: either a console-registered
// template (ID) or an inline custom keyboard (Content).
type MessageKeyboard struct {
	ID      string           `json:"id,omitempty"`
	Content *KeyboardContent `json:"content,omitempty"`
}

// KeyboardContent is a custom inline keyboard: up to 5 rows.
type KeyboardContent struct {
	Rows []KeyboardRow `json:"rows"`
}

// KeyboardRow is a row of up to 5 buttons.
type KeyboardRow struct {
	Buttons []KeyboardButton `json:"buttons"`
}

// KeyboardButton is a single button.
type KeyboardButton struct {
	ID         string       `json:"id,omitempty"`
	RenderData ButtonRender `json:"render_data"`
	Action     ButtonAction `json:"action"`
}

// ButtonRender is a button's appearance.
type ButtonRender struct {
	Label        string `json:"label"`
	VisitedLabel string `json:"visited_label"`
	Style        int    `json:"style"`
}

// ButtonAction is what a button does when tapped.
type ButtonAction struct {
	Type          int              `json:"type"`
	Permission    ButtonPermission `json:"permission"`
	Data          string           `json:"data"`
	Enter         bool             `json:"enter,omitempty"` // auto-send Data on click (C2C only)
	Reply         bool             `json:"reply,omitempty"`
	UnsupportTips string           `json:"unsupport_tips"`
}

// ButtonPermission controls who may click a button.
type ButtonPermission struct {
	Type int `json:"type"`
}
