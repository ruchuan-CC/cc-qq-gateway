package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-qq-gateway/internal/codex"
	"github.com/chenhg5/cc-qq-gateway/internal/config"
	"github.com/chenhg5/cc-qq-gateway/internal/qq"
	"github.com/chenhg5/cc-qq-gateway/internal/session"
)

type fakeCodexRunner struct {
	result *codex.Result
	err    error
	seen   chan codex.Request
}

func (f *fakeCodexRunner) Run(_ context.Context, req codex.Request) (*codex.Result, error) {
	f.seen <- req
	return f.result, f.err
}

type fakeQQSender struct {
	seen chan *qq.MessageRequest
}

func (f *fakeQQSender) SendC2CMessage(_ context.Context, _ string, req *qq.MessageRequest) (*qq.MessageResponse, error) {
	cp := *req
	f.seen <- &cp
	return &qq.MessageResponse{ID: "sent"}, nil
}

func TestC2CTextIsPassedToCodexWithoutGatewayCommandHandling(t *testing.T) {
	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "codex reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 1),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 1)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{MaxReplyChars: 1800}, log.New(io.Discard, "", 0))

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-1", "user-1", "/status", nil))

	select {
	case req := <-runner.seen:
		if req.Prompt != "/status" {
			t.Fatalf("prompt = %q, want /status", req.Prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called")
	}

	select {
	case msg := <-sender.seen:
		if msg.Content != "codex reply" {
			t.Fatalf("reply content = %q, want codex reply", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("QQ reply was not sent")
	}
}

func TestTextWithAttachmentPassesLocalPathToCodex(t *testing.T) {
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image-bytes"))
	}))
	defer fileServer.Close()

	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "codex reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 1),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 1)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{
		MaxReplyChars:      1800,
		AttachmentDir:      t.TempDir(),
		AttachmentMaxBytes: 1024,
	}, log.New(io.Discard, "", 0))

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-2", "user-1", "看这张图", []qq.MessageAttachment{{
		Filename:    "photo.png",
		ContentType: "image/png",
		URL:         fileServer.URL + "/photo.png",
	}}))

	select {
	case req := <-runner.seen:
		if !strings.Contains(req.Prompt, "看这张图") {
			t.Fatalf("prompt missing original text: %q", req.Prompt)
		}
		if !strings.Contains(req.Prompt, "QQ 附件：") {
			t.Fatalf("prompt missing attachment section: %q", req.Prompt)
		}
		if !strings.Contains(req.Prompt, "image/png: ") || !strings.Contains(req.Prompt, "photo.png") {
			t.Fatalf("prompt missing local attachment path: %q", req.Prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called")
	}
}

func TestAttachmentOnlyWaitsForNextText(t *testing.T) {
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pdf-bytes"))
	}))
	defer fileServer.Close()

	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "codex reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 1),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 2)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{
		MaxReplyChars:      1800,
		AttachmentDir:      t.TempDir(),
		AttachmentMaxBytes: 1024,
	}, log.New(io.Discard, "", 0))

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-3", "user-1", "", []qq.MessageAttachment{{
		Filename:    "report.pdf",
		ContentType: "application/pdf",
		URL:         fileServer.URL + "/report.pdf",
	}}))

	select {
	case req := <-runner.seen:
		t.Fatalf("Codex runner called for attachment-only message: %+v", req)
	case <-time.After(150 * time.Millisecond):
	}
	select {
	case msg := <-sender.seen:
		if !strings.Contains(msg.Content, "附件已保存") {
			t.Fatalf("attachment-only reply = %q, want saved prompt", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("QQ prompt for follow-up text was not sent")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-4", "user-1", "总结一下", nil))

	select {
	case req := <-runner.seen:
		if !strings.Contains(req.Prompt, "总结一下") || !strings.Contains(req.Prompt, "report.pdf") {
			t.Fatalf("next prompt did not include text and pending attachment: %q", req.Prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called for follow-up text")
	}
}

func TestAttachmentDownloadFailureReachesCodex(t *testing.T) {
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer fileServer.Close()

	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "codex reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 1),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 1)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{
		MaxReplyChars:      1800,
		AttachmentDir:      t.TempDir(),
		AttachmentMaxBytes: 1024,
	}, log.New(io.Discard, "", 0))

	badURL := fileServer.URL + "/missing.png"
	g.HandleEvent(context.Background(), c2cPayload(t, "msg-5", "user-1", "分析附件", []qq.MessageAttachment{{
		Filename:    "missing.png",
		ContentType: "image/png",
		URL:         badURL,
	}}))

	select {
	case req := <-runner.seen:
		if !strings.Contains(req.Prompt, "download failed") || !strings.Contains(req.Prompt, badURL) {
			t.Fatalf("prompt missing failed attachment metadata: %q", req.Prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called")
	}
}

func c2cPayload(t *testing.T, msgID, openID, content string, atts []qq.MessageAttachment) *qq.Payload {
	t.Helper()
	raw, err := json.Marshal(qq.C2CMessage{
		ID:          msgID,
		Content:     content,
		Author:      qq.C2CMessageAuthor{UserOpenID: openID},
		Attachments: atts,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &qq.Payload{Type: qq.EventC2CMessageCreate, Data: raw}
}
