package claude

import "testing"

func TestNormalizeModel(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		// the reported bug: the app's display name (incl. full-width parens) must map,
		// not pass through and 404.
		{"Opus 4.8 (1M context)", "claude-opus-4-8[1m]", true},
		{"Opus 4.8 （1M context）", "claude-opus-4-8[1m]", true},
		{"opus 1m", "claude-opus-4-8[1m]", true},
		{"opus 4.8", "opus", true},
		{"Opus", "opus", true},
		{"sonnet", "sonnet", true},
		{"Sonnet 4.6", "sonnet", true},
		{"haiku", "haiku", true},
		{"fable 5", "fable", true},
		// explicit ids and the default/reset forms
		{"claude-opus-4-8[1m]", "claude-opus-4-8[1m]", true},
		{"claude-sonnet-4-6", "claude-sonnet-4-6", true},
		{"", "", true},
		{"default", "", true},
		{"reset", "", true},
		{"默认", "", true},
		// a bare unknown token is allowed through (could be a new CLI alias/id)
		{"opusplan", "opusplan", true},
		{"some-future-alias", "some-future-alias", true},
		// multi-word junk we can't map is rejected (so it never wedges the session)
		{"the big smart one", "", false},
		{"please use gpt-4 (turbo)", "", false},
		// a stray non-ASCII word (e.g. from "model 列表") is NOT a model id and must be
		// rejected, not stored and left to 404 on the next turn.
		{"列表", "", false},
		{"模型", "", false},
	}
	for _, c := range cases {
		got, ok := NormalizeModel(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("NormalizeModel(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
