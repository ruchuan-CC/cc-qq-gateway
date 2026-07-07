package gateway

import (
	"strings"
	"unicode"
)

type commandKind int

const (
	commandNone commandKind = iota
	commandHelp
	commandModel
	commandPermissions
	commandPlan
	commandGoal
)

type command struct {
	kind commandKind
	arg  string
}

// parseCommand 只识别 QQ 侧保留的 5 个英文指令；未知 /xxx 必须继续交给 Codex。
func parseCommand(text string) (command, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") {
		return command{}, false
	}
	body := strings.TrimPrefix(trimmed, "/")
	name, arg, _ := strings.Cut(body, " ")
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "help":
		return command{kind: commandHelp, arg: strings.TrimSpace(arg)}, true
	case "model":
		return command{kind: commandModel, arg: strings.TrimSpace(arg)}, true
	case "permissions":
		return command{kind: commandPermissions, arg: strings.TrimSpace(arg)}, true
	case "plan":
		return command{kind: commandPlan, arg: strings.TrimSpace(arg)}, true
	case "goal":
		return command{kind: commandGoal, arg: strings.TrimSpace(arg)}, true
	default:
		return command{}, false
	}
}

func unescapeCommandText(text string) string {
	prefixLen := 0
	for _, r := range text {
		if !unicode.IsSpace(r) {
			break
		}
		prefixLen += len(string(r))
	}
	if strings.HasPrefix(text[prefixLen:], "//") {
		return text[:prefixLen] + text[prefixLen+1:]
	}
	return text
}

func helpText() string {
	return strings.TrimSpace(`# QQ-Codex 使用说明

## 基础用法

- **普通消息**：直接发给 Codex。
- **附件**：图片、文件、视频会先保存到本地，再把路径交给 Codex。

## 核心指令

- **/model gpt-5.5**：切换模型。
- **/model default**：恢复默认模型。
- **/permissions read-only**：只读分析。
- **/permissions workspace-write**：允许修改工作目录。
- **/permissions full-access**：全权限，仅管理员可用。
- **/permissions default**：恢复默认权限。
- **/plan 需求**：单次只读规划，不改变后续权限。
- **/goal 目标**：设置当前会话目标。
- **/goal show**：查看当前会话目标。
- **/goal clear**：清除当前会话目标。

## 转义

- **//model gpt-5.5**：把 /model 当普通内容发给 Codex。`)
}

func validPermissions(v string) bool {
	switch v {
	case "read-only", "workspace-write", "full-access":
		return true
	default:
		return false
	}
}
