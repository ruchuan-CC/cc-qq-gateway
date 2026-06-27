package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chenhg5/cc-qq-gateway/internal/qq"
)

// ProtocolPrompt is injected into Claude's system prompt on every turn so it
// knows how to exchange rich media with the QQ user — giving the QQ surface the
// same multimodal reach as the Claude app / local Claude Code.
const ProtocolPrompt = `You are reachable over QQ, and the gateway gives you full multimodal I/O:

INPUT: When the user sends images or files, the gateway downloads them and lists
their local paths in the message (under "[Attachments saved locally: ...]"). Use
the Read tool on those paths to view images or read file contents — treat them
exactly like attachments in the Claude app.

OUTPUT: To send a file or image BACK to the user over QQ, print a line by itself
in EXACTLY one of these forms (absolute local path or an http(s) URL). The
gateway removes these lines from your text reply and delivers the media:
  @@QQ_IMAGE: /abs/path/or/https/url
  @@QQ_FILE:  /abs/path/or/https/url
  @@QQ_VIDEO: /abs/path/or/https/url
  @@QQ_AUDIO: /abs/path/or/https/url
For example, after generating a chart at /home/claude/out.png, end your reply with
a line: @@QQ_IMAGE: /home/claude/out.png

Long replies are delivered in full (as an attached file when they exceed the chat
limit), so you don't need to truncate — but prefer clear, chat-friendly answers.

FORMATTING: You may use Markdown in your replies — headings, **bold**, *italic*,
lists, > quotes, ` + "`code`" + ` and fenced code blocks, and [links](url). The gateway
renders them as QQ markdown messages (falling back to plain text automatically if
the bot isn't approved for markdown), so format naturally as you would in the
Claude app. Keep replies clear and skimmable; avoid pipe tables (QQ doesn't render
them reliably) — use bold labels and short lines instead.

CONFIRMATION: Before any destructive or irreversible action — deleting or
overwriting important data, removing many files, wiping/reformatting, dropping
databases, system-wide or security-affecting changes, or anything you cannot
easily undo — first send a concise message stating exactly what you are about to
do and ask the user to confirm (reply 确认/是 to proceed, 取消/否 to abort), then
END YOUR TURN. Only carry out the action after the user confirms in their next
message. For routine, reversible work, just proceed — don't ask for confirmation
unnecessarily.`

// mediaItem is one piece of outbound media to deliver to the user.
type mediaItem struct {
	kind int    // qq.FileType*
	path string // local absolute path (preferred if set)
	url  string // http(s) URL
}

func (m mediaItem) ref() string {
	if m.path != "" {
		return m.path
	}
	return m.url
}

// directiveRe matches outbound media directives like "@@QQ_IMAGE: /path".
var directiveRe = regexp.MustCompile(`(?mi)^[ \t]*@@QQ_(IMAGE|FILE|VIDEO|AUDIO):[ \t]*(\S.*?)[ \t]*$`)

// extractSendDirectives pulls @@QQ_* directives out of the reply text, returning
// the cleaned text (directives removed) and the media items to deliver.
func extractSendDirectives(text string) (string, []mediaItem) {
	matches := directiveRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return text, nil
	}
	var items []mediaItem
	for _, m := range matches {
		kind := qq.FileTypeFile
		switch strings.ToUpper(m[1]) {
		case "IMAGE":
			kind = qq.FileTypeImage
		case "VIDEO":
			kind = qq.FileTypeVideo
		case "AUDIO":
			kind = qq.FileTypeAudio
		case "FILE":
			kind = qq.FileTypeFile
		}
		ref := strings.TrimSpace(m[2])
		it := mediaItem{kind: kind}
		if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
			it.url = ref
		} else {
			it.path = ref
		}
		items = append(items, it)
	}
	cleaned := strings.TrimSpace(directiveRe.ReplaceAllString(text, ""))
	return cleaned, items
}

// materializeAttachments downloads inbound attachments to the media dir and
// returns a note (to append to the prompt) listing their local paths so Claude
// can Read them. Best-effort: download failures fall back to the URL.
func (g *Gateway) materializeAttachments(ctx context.Context, key string, atts []qq.MessageAttachment) string {
	if len(atts) == 0 {
		return ""
	}
	dir := filepath.Join(g.cfg.MediaDir, sanitize(key))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		g.logger.Printf("[media] mkdir %s: %v", dir, err)
	}
	var b strings.Builder
	b.WriteString("[Attachments saved locally — use the Read tool to view/analyze them:")
	for i, a := range atts {
		url := normalizeURL(a.URL)
		name := safeFilename(a.Filename, a.ContentType, i)
		dest := filepath.Join(dir, fmt.Sprintf("%d_%s", i+1, name))
		if err := downloadFile(ctx, url, dest); err != nil {
			g.logger.Printf("[media] download %s: %v", url, err)
			b.WriteString(fmt.Sprintf("\n - %s (download failed; URL: %s)", name, url))
			continue
		}
		kind := a.ContentType
		if kind == "" {
			kind = "file"
		}
		b.WriteString(fmt.Sprintf("\n - %s [%s] -> %s", name, kind, dest))
	}
	b.WriteString("]")
	return b.String()
}

// writeReplyFile stages a long text reply as a Markdown file for upload.
func (g *Gateway) writeReplyFile(key, text string) (string, error) {
	dir := filepath.Join(g.cfg.MediaDir, sanitize(key))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "reply.md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func downloadFile(ctx context.Context, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, io.LimitReader(resp.Body, 100<<20)) // 100 MiB cap
	return err
}

func normalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return u
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "https://" + u
	}
	return u
}

// safeFilename derives a filesystem-safe name, inventing an extension from the
// content type when the attachment has no usable filename.
func safeFilename(name, contentType string, idx int) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name != "" && name != "." && name != "/" {
		return sanitize(name)
	}
	ext := ".bin"
	switch {
	case strings.Contains(contentType, "png"):
		ext = ".png"
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		ext = ".jpg"
	case strings.Contains(contentType, "gif"):
		ext = ".gif"
	case strings.Contains(contentType, "webp"):
		ext = ".webp"
	case strings.Contains(contentType, "silk"), strings.Contains(contentType, "audio"):
		ext = ".silk"
	case strings.Contains(contentType, "mp4"), strings.Contains(contentType, "video"):
		ext = ".mp4"
	case strings.Contains(contentType, "image"):
		ext = ".img"
	}
	return fmt.Sprintf("attachment_%d%s", idx+1, ext)
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitize(s string) string {
	s = unsafeChars.ReplaceAllString(s, "_")
	if len(s) > 80 {
		s = s[:80]
	}
	if s == "" {
		return "x"
	}
	return s
}
