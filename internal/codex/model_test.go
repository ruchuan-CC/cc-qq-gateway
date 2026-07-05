package codex

import "testing"

func TestNormalizeModel(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"gpt 5.5", "gpt-5.5", true},
		{"gpt5.5", "gpt-5.5", true},
		{"gpt-5.5", "gpt-5.5", true},
		{"o3", "o3", true},
		{"some-future-alias", "some-future-alias", true},
		{"opus-4-8", "", false},
		{"sonnet", "", false},
		{"fable", "", false},
		{"", "", true},
		{"default", "", true},
		{"reset", "", true},
		{"默认", "", true},
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
