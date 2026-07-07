package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chenhg5/cc-qq-gateway/internal/qq"
	"github.com/chenhg5/cc-qq-gateway/internal/session"
)

func (g *Gateway) materializeAttachments(ctx context.Context, key, msgID string, atts []qq.MessageAttachment) []session.AttachmentRef {
	if len(atts) == 0 {
		return nil
	}
	dir := filepath.Join(g.attachmentDir(), sanitizePathPart(key), sanitizePathPart(nonEmpty(msgID, fmt.Sprintf("%d", time.Now().UnixNano()))))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		g.logger.Printf("[attachments] mkdir %s: %v", dir, err)
	}

	refs := make([]session.AttachmentRef, 0, len(atts))
	for i, a := range atts {
		kind := strings.TrimSpace(a.ContentType)
		if kind == "" {
			kind = "file"
		}
		url := normalizeAttachmentURL(a.URL)
		ref := session.AttachmentRef{Kind: kind, URL: url}
		if url == "" {
			ref.Error = "download failed: empty attachment url"
			refs = append(refs, ref)
			continue
		}
		if g.attachmentMaxBytes() > 0 && a.Size > g.attachmentMaxBytes() {
			ref.Error = fmt.Sprintf("download failed: attachment size %d exceeds max %d", a.Size, g.attachmentMaxBytes())
			refs = append(refs, ref)
			continue
		}

		name := safeAttachmentName(a.Filename, kind, i)
		dest := filepath.Join(dir, fmt.Sprintf("%d_%s", i+1, name))
		if err := downloadAttachment(ctx, url, dest, g.attachmentMaxBytes()); err != nil {
			ref.Error = "download failed: " + err.Error()
			g.logger.Printf("[attachments] download %s: %v", url, err)
		} else {
			ref.Path = dest
		}
		refs = append(refs, ref)
	}
	return refs
}

func composePrompt(text string, refs []session.AttachmentRef) string {
	text = strings.TrimSpace(text)
	if len(refs) == 0 {
		return text
	}
	var b strings.Builder
	if text != "" {
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	b.WriteString("QQ 附件：")
	for _, ref := range refs {
		kind := strings.TrimSpace(ref.Kind)
		if kind == "" {
			kind = "file"
		}
		if ref.Path != "" {
			b.WriteString("\n- ")
			b.WriteString(kind)
			b.WriteString(": ")
			b.WriteString(ref.Path)
			continue
		}
		b.WriteString("\n- ")
		b.WriteString(kind)
		b.WriteString(": download failed")
		if ref.URL != "" {
			b.WriteString(", url=")
			b.WriteString(ref.URL)
		}
		if ref.Error != "" {
			b.WriteString(", error=")
			b.WriteString(ref.Error)
		}
	}
	return b.String()
}

func (g *Gateway) attachmentDir() string {
	if g.cfg.AttachmentDir != "" {
		return g.cfg.AttachmentDir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/tmp"
	}
	return filepath.Join(home, ".cc-qq", "attachments")
}

func (g *Gateway) attachmentMaxBytes() int64 {
	if g.cfg.AttachmentMaxBytes > 0 {
		return g.cfg.AttachmentMaxBytes
	}
	return 512 * 1024 * 1024
}

func downloadAttachment(ctx context.Context, url, dest string, maxBytes int64) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
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
	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return fmt.Errorf("content length %d exceeds max %d", resp.ContentLength, maxBytes)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	var r io.Reader = resp.Body
	if maxBytes > 0 {
		r = io.LimitReader(resp.Body, maxBytes+1)
	}
	n, err := io.Copy(f, r)
	if err != nil {
		return err
	}
	if maxBytes > 0 && n > maxBytes {
		return fmt.Errorf("download exceeds max %d", maxBytes)
	}
	return nil
}

func normalizeAttachmentURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return "https://" + u
}

func safeAttachmentName(name, contentType string, idx int) string {
	name = sanitizePathPart(filepath.Base(strings.TrimSpace(name)))
	if name != "" && name != "." {
		return name
	}
	return fmt.Sprintf("attachment_%d%s", idx+1, attachmentExt(contentType))
}

func attachmentExt(contentType string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		return ".jpg"
	case strings.Contains(ct, "gif"):
		return ".gif"
	case strings.Contains(ct, "webp"):
		return ".webp"
	case strings.Contains(ct, "pdf"):
		return ".pdf"
	case strings.Contains(ct, "mp4"), strings.Contains(ct, "video"):
		return ".mp4"
	case strings.Contains(ct, "audio"), strings.Contains(ct, "silk"):
		return ".silk"
	case strings.Contains(ct, "image"):
		return ".img"
	default:
		return ".bin"
	}
}

func sanitizePathPart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return "unknown"
	}
	return out
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
