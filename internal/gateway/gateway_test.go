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

func TestHelpCommandRepliesWithoutCallingCodex(t *testing.T) {
	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "codex reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 1),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 1)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{MaxReplyChars: 1800}, log.New(io.Discard, "", 0))

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-help", "user-1", "/help", nil))

	select {
	case req := <-runner.seen:
		t.Fatalf("Codex runner called for /help: %+v", req)
	case <-time.After(150 * time.Millisecond):
	}
	select {
	case msg := <-sender.seen:
		if !strings.Contains(msg.Content, "QQ-Codex 使用说明") {
			t.Fatalf("help reply = %q, want Chinese usage text", msg.Content)
		}
		for _, want := range []string{"基础用法", "核心指令", "/permissions workspace-write", "/goal clear", "//model gpt-5.5"} {
			if !strings.Contains(msg.Content, want) {
				t.Fatalf("help reply = %q, want to contain %q", msg.Content, want)
			}
		}
		if strings.Contains(msg.Content, "<") || strings.Contains(msg.Content, ">") || strings.Contains(msg.Content, "`") {
			t.Fatalf("help reply = %q, should avoid unsupported inline-code/placeholders", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("QQ help reply was not sent")
	}
}

func TestModelCommandPersistsOverrideForNextTurn(t *testing.T) {
	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "codex reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 1),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 2)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{MaxReplyChars: 1800}, log.New(io.Discard, "", 0))

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-model", "user-1", "/model gpt-5.5", nil))

	select {
	case req := <-runner.seen:
		t.Fatalf("Codex runner called for /model: %+v", req)
	case <-time.After(150 * time.Millisecond):
	}
	select {
	case msg := <-sender.seen:
		if !strings.Contains(msg.Content, "模型已切换") || !strings.Contains(msg.Content, "gpt-5.5") {
			t.Fatalf("model reply = %q, want switch confirmation", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("QQ model reply was not sent")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-next", "user-1", "继续", nil))

	select {
	case req := <-runner.seen:
		if req.Model != "gpt-5.5" {
			t.Fatalf("request model = %q, want gpt-5.5", req.Model)
		}
		if req.Prompt != "继续" {
			t.Fatalf("prompt = %q, want ordinary text", req.Prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called for next turn")
	}
}

func TestModelDefaultClearsOverride(t *testing.T) {
	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "codex reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 1),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 3)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{MaxReplyChars: 1800}, log.New(io.Discard, "", 0))

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-model", "user-1", "/model gpt-5.5", nil))
	select {
	case <-sender.seen:
	case <-time.After(time.Second):
		t.Fatal("QQ model reply was not sent")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-default", "user-1", "/model default", nil))
	select {
	case msg := <-sender.seen:
		if !strings.Contains(msg.Content, "模型已恢复") {
			t.Fatalf("model default reply = %q, want restore confirmation", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("QQ model default reply was not sent")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-next", "user-1", "继续", nil))
	select {
	case req := <-runner.seen:
		if req.Model != "" {
			t.Fatalf("request model = %q, want config default", req.Model)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called for next turn")
	}
}

func TestPermissionsCommandMapsAndRestrictsFullAccess(t *testing.T) {
	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "codex reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 1),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 3)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{
		MaxReplyChars: 1800,
		AdminUsers:    []string{"admin-user"},
	}, log.New(io.Discard, "", 0))

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-deny", "user-1", "/permissions full-access", nil))

	select {
	case req := <-runner.seen:
		t.Fatalf("Codex runner called for denied /permissions: %+v", req)
	case <-time.After(150 * time.Millisecond):
	}
	select {
	case msg := <-sender.seen:
		if !strings.Contains(msg.Content, "只有管理员") {
			t.Fatalf("deny reply = %q, want admin warning", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("QQ deny reply was not sent")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-perm", "admin-user", "/permissions workspace-write", nil))
	select {
	case msg := <-sender.seen:
		if !strings.Contains(msg.Content, "权限已切换") || !strings.Contains(msg.Content, "workspace-write") {
			t.Fatalf("permissions reply = %q, want switch confirmation", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("QQ permissions reply was not sent")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-next", "admin-user", "改一下 README", nil))
	select {
	case req := <-runner.seen:
		if req.Permissions != "workspace-write" {
			t.Fatalf("request permissions = %q, want workspace-write", req.Permissions)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called for next turn")
	}
}

func TestPlanCommandUsesOneShotReadOnlyMode(t *testing.T) {
	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "plan reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 2),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 3)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{MaxReplyChars: 1800}, log.New(io.Discard, "", 0))

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-perm", "user-1", "/permissions workspace-write", nil))
	select {
	case <-sender.seen:
	case <-time.After(time.Second):
		t.Fatal("QQ permissions reply was not sent")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-plan", "user-1", "/plan 分析项目结构", nil))
	select {
	case req := <-runner.seen:
		if !req.PlanOnly {
			t.Fatalf("/plan request PlanOnly = false, want true")
		}
		if req.Permissions != "workspace-write" {
			t.Fatalf("/plan should not mutate stored permissions before request, got %q", req.Permissions)
		}
		if req.Prompt != "分析项目结构" {
			t.Fatalf("/plan prompt = %q, want stripped requirement", req.Prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called for /plan")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-next", "user-1", "现在执行", nil))
	select {
	case req := <-runner.seen:
		if req.PlanOnly {
			t.Fatalf("ordinary next turn PlanOnly = true, want false")
		}
		if req.Permissions != "workspace-write" {
			t.Fatalf("stored permissions after /plan = %q, want workspace-write", req.Permissions)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called for next turn")
	}
}

func TestGoalCommandAddsContextToFollowingTurns(t *testing.T) {
	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "codex reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 1),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 2)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{MaxReplyChars: 1800}, log.New(io.Discard, "", 0))

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-goal", "user-1", "/goal 做一个长期在线的 QQ-Codex 网关", nil))
	select {
	case msg := <-sender.seen:
		if !strings.Contains(msg.Content, "目标已设置") {
			t.Fatalf("goal reply = %q, want set confirmation", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("QQ goal reply was not sent")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-next", "user-1", "下一步", nil))
	select {
	case req := <-runner.seen:
		if req.Goal != "做一个长期在线的 QQ-Codex 网关" {
			t.Fatalf("request goal = %q, want saved goal", req.Goal)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called for next turn")
	}
}

func TestGoalShowAndClear(t *testing.T) {
	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "codex reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 1),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 4)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{MaxReplyChars: 1800}, log.New(io.Discard, "", 0))

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-goal", "user-1", "/goal 长期在线", nil))
	select {
	case <-sender.seen:
	case <-time.After(time.Second):
		t.Fatal("QQ goal set reply was not sent")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-show", "user-1", "/goal show", nil))
	select {
	case msg := <-sender.seen:
		if !strings.Contains(msg.Content, "长期在线") {
			t.Fatalf("goal show reply = %q, want saved goal", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("QQ goal show reply was not sent")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-clear", "user-1", "/goal clear", nil))
	select {
	case msg := <-sender.seen:
		if !strings.Contains(msg.Content, "目标已清除") {
			t.Fatalf("goal clear reply = %q, want clear confirmation", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("QQ goal clear reply was not sent")
	}

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-next", "user-1", "下一步", nil))
	select {
	case req := <-runner.seen:
		if req.Goal != "" {
			t.Fatalf("request goal = %q, want cleared goal", req.Goal)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called for next turn")
	}
}

func TestEscapedCoreCommandIsSentToCodex(t *testing.T) {
	runner := &fakeCodexRunner{
		result: &codex.Result{Text: "codex reply", SessionID: "thread-1"},
		seen:   make(chan codex.Request, 1),
	}
	sender := &fakeQQSender{seen: make(chan *qq.MessageRequest, 1)}
	g := New(sender, runner, session.NewManager(), config.GatewayConfig{MaxReplyChars: 1800}, log.New(io.Discard, "", 0))

	g.HandleEvent(context.Background(), c2cPayload(t, "msg-escape", "user-1", "//model gpt-5.5", nil))

	select {
	case req := <-runner.seen:
		if req.Prompt != "/model gpt-5.5" {
			t.Fatalf("escaped prompt = %q, want /model gpt-5.5", req.Prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex runner was not called")
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
