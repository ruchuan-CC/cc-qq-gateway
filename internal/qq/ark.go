package qq

// ARK is QQ's structured template-card message (msg_type=3). It is supported in
// single-chat (C2C) and group. Active ARK is open by default; PASSIVE ARK (a reply
// bound to an inbound msg_id) requires platform approval in the bot console.
// Built-in templates include: 23 (text + a vertical list of {desc, link} rows),
// 24 (text + thumbnail), 37 (big image card).

// ArkTemplateList is template 23: a header (#DESC#/#PROMPT#) plus a #LIST# of rows.
const ArkTemplateList = 23

// MessageArk is the ark field of a message.
type MessageArk struct {
	TemplateID int     `json:"template_id"`
	KV         []ArkKV `json:"kv,omitempty"`
}

// ArkKV is one template variable: a scalar Value, or an Obj array for list (#LIST#)
// variables.
type ArkKV struct {
	Key   string   `json:"key"`
	Value string   `json:"value,omitempty"`
	Obj   []ArkObj `json:"obj,omitempty"`
}

// ArkObj is one row of a list variable.
type ArkObj struct {
	ObjKV []ArkObjKV `json:"obj_kv,omitempty"`
}

// ArkObjKV is a key/value within a list row.
type ArkObjKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
