package codex

import (
	"strings"
	"testing"
)

func TestRequestOverridesModelAndPermissions(t *testing.T) {
	b := New(Config{
		Model:          "config-model",
		Sandbox:        "read-only",
		ApprovalPolicy: "never",
	})

	args := b.execArgs(Request{
		Model:       "gpt-5.5",
		Permissions: "workspace-write",
	})
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--model gpt-5.5") {
		t.Fatalf("args = %v, want request model override", args)
	}
	if !strings.Contains(joined, "--sandbox workspace-write") {
		t.Fatalf("args = %v, want workspace-write sandbox", args)
	}
	if !strings.Contains(joined, "--ask-for-approval never") {
		t.Fatalf("args = %v, want never approval", args)
	}
}

func TestFullAccessPermissionsBypassSandboxAndApproval(t *testing.T) {
	b := New(Config{
		Sandbox:        "read-only",
		ApprovalPolicy: "never",
	})

	args := b.execArgs(Request{Permissions: "full-access"})
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("args = %v, want bypass flag", args)
	}
	if strings.Contains(joined, "--sandbox") || strings.Contains(joined, "--ask-for-approval") {
		t.Fatalf("args = %v, full-access must not also pass sandbox/approval", args)
	}
}

func TestPlanOnlyRequestForcesReadOnlyWithoutPersistedPermission(t *testing.T) {
	b := New(Config{
		Sandbox:        "workspace-write",
		ApprovalPolicy: "never",
	})

	args := b.execArgs(Request{
		Permissions: "workspace-write",
		PlanOnly:    true,
	})
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--sandbox read-only") {
		t.Fatalf("args = %v, want plan-only read-only sandbox", args)
	}
	if !strings.Contains(joined, "--ask-for-approval never") {
		t.Fatalf("args = %v, want plan-only never approval", args)
	}
}

func TestPromptAddsGoalAndPlanInstruction(t *testing.T) {
	b := New(Config{AppendSystemPrompt: "system guidance"})

	got := b.prompt(Request{
		Prompt:   "分析项目",
		Goal:     "长期在线",
		PlanOnly: true,
	})

	for _, want := range []string{"system guidance", "当前 QQ 会话目标：", "长期在线", "只制定方案", "分析项目"} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt = %q, want to contain %q", got, want)
		}
	}
}
