package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

func TestParseAIResponseToolCall(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"bot_command","arguments":"{\"command\":\"Find\",\"args\":\"cpu>80 tag=hk 24h\"}"}}]}}]}`)
	d, err := parseAIResponse(body, 200)
	if err != nil {
		t.Fatal(err)
	}
	if d.Command != "find" || d.Args != "cpu>80 tag=hk 24h" {
		t.Fatalf("decision = %#v", d)
	}
}

func TestParseAIResponseNoopAndEmpty(t *testing.T) {
	noop := []byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"noop","arguments":"{\"reply\":\"我只懂监控查询\"}"}}]}}]}`)
	d, err := parseAIResponse(noop, 200)
	if err != nil {
		t.Fatal(err)
	}
	if d.Command != "" || d.Hint != "我只懂监控查询" {
		t.Fatalf("noop decision = %#v", d)
	}
	empty := []byte(`{"choices":[{"message":{"content":"你好"}}]}`)
	if d, err := parseAIResponse(empty, 200); err != nil || d.Command != "" {
		t.Fatalf("empty = %#v, %v", d, err)
	}
	if _, err := parseAIResponse([]byte(`{"error":{"message":"bad key"}}`), 401); err == nil {
		t.Fatal("expected error for api error payload")
	}
}

// AI 只能映射注册表 AI=true 的只读命令，写操作与越权一律拒绝。
func TestApplyAIDecisionGuardrails(t *testing.T) {
	h := &Hub{sender: &Sender{ch: make(chan outbound, 8)}}
	viewer := context.WithValue(context.Background(), ctxChat, &model.BotChat{ChatID: 42, Role: model.BotRoleViewer})

	if h.applyAIDecision(viewer, aiDecision{Command: "mute", Args: "hk-1"}) {
		t.Fatal("mute must not be AI-triggerable")
	}
	assertReply(t, h, "该操作不支持通过 AI 触发，请使用对应命令。")

	if h.applyAIDecision(viewer, aiDecision{Command: "chats"}) {
		t.Fatal("admin command must not be AI-triggerable")
	}
	assertReply(t, h, "该操作不支持通过 AI 触发，请使用对应命令。")

	if h.applyAIDecision(viewer, aiDecision{Hint: "不明白"}) {
		t.Fatal("noop should not dispatch")
	}
	assertReply(t, h, "不明白")

	spec := lookupCommand("status")
	orig := spec.Handler
	defer func() { spec.Handler = orig }()
	called := ""
	spec.Handler = func(h *Hub, ctx context.Context, arg string) { called = arg }
	if !h.applyAIDecision(viewer, aiDecision{Command: "status"}) {
		t.Fatal("status should dispatch")
	}
	if called != "" {
		t.Fatalf("status arg = %q", called)
	}
}

func TestAISystemPromptOnlyListsReadonlyCommands(t *testing.T) {
	prompt := aiSystemPrompt()
	lines := map[string]bool{}
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "/") {
			lines[strings.Fields(strings.TrimPrefix(line, "/"))[0]] = true
		}
	}
	for i := range commandSpecs {
		spec := &commandSpecs[i]
		if spec.AI && !lines[spec.Name] {
			t.Fatalf("AI command /%s missing from prompt", spec.Name)
		}
		if !spec.AI && lines[spec.Name] {
			t.Fatalf("non-AI command /%s leaked into prompt", spec.Name)
		}
	}
}

func TestAINormalize(t *testing.T) {
	var cfg model.AIConfig
	cfg.Normalize()
	if cfg.BaseURL == "" || cfg.Model == "" || cfg.RatePerMinute != 10 {
		t.Fatalf("normalize defaults: %#v", cfg)
	}
	singleton.Conf = &model.Config{}
	cfg.Enabled = true
	singleton.Conf.Bot.AI = cfg
	if !Shared().aiEnabled() {
		t.Fatal("aiEnabled should be true with defaults+enabled")
	}
	singleton.Conf.Bot.AI.Enabled = false
	if Shared().aiEnabled() {
		t.Fatal("aiEnabled should be false when disabled")
	}
	singleton.Conf = nil
	if Shared().aiEnabled() {
		t.Fatal("aiEnabled should be false without conf")
	}
}

func TestParseAIStreamToolCallDeltas(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"用户想查香港高CPU\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"bot_command\",\"arguments\":\"{\\\"command\\\":\\\"find\\\"\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\",\\\"args\\\":\\\"tag=hk cpu>0 24h sort=-cpu\\\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n"
	d, err := parseAIStream([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if d.Command != "find" {
		t.Fatalf("assembled command = %q", d.Command)
	}
	if d.Args != "tag=hk cpu>0 24h sort=-cpu" {
		t.Fatalf("args = %q", d.Args)
	}
}

func TestParseAIStreamFallbacks(t *testing.T) {
	// 纯文本回答（思考模型散文作答）→ 提示语，不执行任何命令
	prose := "data: {\"choices\":[{\"delta\":{\"content\":\"我只能帮你查监控\"}}]}\n\ndata: [DONE]\n"
	d, err := parseAIStream([]byte(prose))
	if err != nil {
		t.Fatal(err)
	}
	if d.Command != "" || d.Hint != "我只能帮你查监控" {
		t.Fatalf("prose = %#v", d)
	}
	// 流中报错
	if _, err := parseAIStream([]byte("data: {\"error\":{\"message\":\"quota\"}}\n\n")); err == nil {
		t.Fatal("expected stream error")
	}
	// 服务端不理会 stream，回整段 JSON：aiResolve 已按首字节分流，这里直接验非流路径
	plain := []byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"bot_command","arguments":"{\"command\":\"usage\",\"args\":\"today\"}"}}]}}]}`)
	if d, err := parseAIResponse(plain, 200); err != nil || d.Command != "usage" {
		t.Fatalf("plain json = %#v, %v", d, err)
	}
}

func TestParseAIStreamLongContentTruncated(t *testing.T) {
	long := strings.Repeat("思", 300)
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"" + long + "\"}}]}\n\ndata: [DONE]\n"
	d, err := parseAIStream([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if runeCount := len([]rune(d.Hint)); runeCount > 161 {
		t.Fatalf("hint length %d not truncated", runeCount)
	}
}
