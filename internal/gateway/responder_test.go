package gateway

import (
	"testing"

	"github.com/chenhg5/cc-qq-gateway/internal/qq"
)

// A msgseq-dedup 400 (40054005) must NOT latch the process-wide markdown
// downgrade: it is about sequencing, not markdown approval. Regression guard for
// the bug where one dedup error turned every later reply into raw plain text.
func TestDisablesMarkdown(t *testing.T) {
	cases := []struct {
		name string
		err  *qq.APIError
		want bool
	}{
		{"dedup keeps markdown", &qq.APIError{HTTPStatus: 400, Code: qqErrMsgDeduped}, false},
		{"genuine markdown rejection latches", &qq.APIError{HTTPStatus: 400, Code: 11293}, true},
	}
	for _, c := range cases {
		if got := disablesMarkdown(c.err); got != c.want {
			t.Errorf("%s: disablesMarkdown(code=%d) = %v, want %v", c.name, c.err.Code, got, c.want)
		}
	}
}
