package gateway

import "testing"

func TestIsLoopbackAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8787": true,
		"localhost:8787": true,
		"[::1]:8787":     true,
		"0.0.0.0:8787":   false,
		"":               false,
		"127.0.0.1":      false, // missing port
		"38.246.237.30:8787": false,
	}
	for addr, want := range cases {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestExtractNotifyText(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		contentType string
		want        string
	}{
		{"json text field", `{"text":"平仓战报 +16.57"}`, "application/json", "平仓战报 +16.57"},
		{"json sniffed without header", `{"text":"hi"}`, "", "hi"},
		{"raw plain text", "just a line", "text/plain", "just a line"},
		{"json without text field falls back to raw", `{"foo":"bar"}`, "", `{"foo":"bar"}`},
		{"surrounding whitespace trimmed", "  hello  ", "", "hello"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractNotifyText([]byte(c.body), c.contentType); got != c.want {
				t.Errorf("extractNotifyText(%q) = %q, want %q", c.body, got, c.want)
			}
		})
	}
}
