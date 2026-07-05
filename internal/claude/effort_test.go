package claude

import "testing"

func TestNormalizeEffort(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"low", "low", true},
		{"LOW", "low", true},
		{" high ", "high", true},
		{"med", "medium", true},
		{"medium", "medium", true},
		{"xhigh", "xhigh", true},
		{"x-high", "xhigh", true},
		{"extra", "xhigh", true},
		{"max", "max", true},
		{"default", "", true}, // clears the override
		{"", "", true},        // clears the override
		{"默认", "", true},
		{"turbo", "", false}, // unknown level
		{"9000", "", false},
	}
	for _, c := range cases {
		got, ok := NormalizeEffort(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("NormalizeEffort(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
