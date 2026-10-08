package bot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
	"github.com/hi2shark/santaizi-dashboard/service/report"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

const (
	// aiRequestTimeout 覆盖思考模型（DeepSeek-R1/GLM-Z1 等）的长耗时；流式期间有字节持续到达，网关不易掐断。
	aiRequestTimeout = 120 * time.Second
	aiTestTimeout    = 30 * time.Second
	aiTurnTTL        = 30 * time.Minute
	aiCatalogLimit   = 80
)

type aiTurn struct {
	command string
	args    string
	expire  time.Time
}

// aiDecision 是 LLM 对一条自然语言的解析结果。
type aiDecision struct {
	Command string
	Args    string
	Hint    string
}

type aiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type aiFunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type aiTool struct {
	Type     string               `json:"type"`
	Function aiFunctionDefinition `json:"function"`
}

type aiChatRequest struct {
	Model       string      `json:"model"`
	Messages    []aiMessage `json:"messages"`
	Tools       []aiTool    `json:"tools,omitempty"`
	ToolChoice  string      `json:"tool_choice,omitempty"`
	Temperature float64     `json:"temperature"`
	MaxTokens   int         `json:"max_tokens"`
	Stream      bool        `json:"stream"`
}

type aiChatResponse struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

// aiStreamChunk 是 SSE 流式响应的单个 data 载荷；tool_calls 按分片增量到达。
type aiStreamChunk struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

func (h *Hub) aiEnabled() bool {
	if singleton.Conf == nil {
		return false
	}
	ai := singleton.Conf.Bot.AI
	return ai.Enabled && strings.TrimSpace(ai.BaseURL) != "" && strings.TrimSpace(ai.Model) != ""
}

// handleAIQuery 把自然语言交给 LLM 解析并执行映射的只读命令。
func (h *Hub) handleAIQuery(ctx context.Context, text string) {
	chat := chatFrom(ctx)
	if chat == nil {
		return
	}
	ai := singleton.Conf.Bot.AI
	if !h.aiLimiter.Allow(chat.ChatID, ai.RatePerMinute) {
		h.Reply(chat.ChatID, "查询过于频繁，请稍后再试。")
		return
	}
	// 思考模型可能耗时较长：等待期间持续发「输入中」状态。
	stop := make(chan struct{})
	go h.typingLoop(ctx, chat.ChatID, stop)
	prevCmd, prevArgs, _ := h.lastAITurn(chat.ChatID)
	decision, err := h.aiResolve(ctx, text, prevCmd, prevArgs)
	close(stop)
	if err != nil {
		log.Println("SANTAIZI>> bot ai:", err)
		h.Reply(chat.ChatID, "AI 暂不可用，已改用主机搜索。")
		h.searchHosts(ctx, text)
		return
	}
	h.applyAIDecision(ctx, decision)
}

// typingLoop 每 4 秒发一次 typing（Telegram 单次只显示 5 秒），直到 stop。
func (h *Hub) typingLoop(ctx context.Context, chatID int64, stop <-chan struct{}) {
	h.sendTyping(ctx, chatID)
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.sendTyping(ctx, chatID)
		}
	}
}

func (h *Hub) sendTyping(ctx context.Context, chatID int64) {
	h.mu.Lock()
	client := h.client
	h.mu.Unlock()
	if client == nil {
		return
	}
	_, _ = client.SendChatAction(ctx, &tgbot.SendChatActionParams{ChatID: chatID, Action: models.ChatActionTyping})
}

// applyAIDecision 校验并执行 LLM 给出的指令映射。
// 只放行注册表 AI=true 的只读命令，并按会话角色重查权限（防 middleware 之后的绕过）。
func (h *Hub) applyAIDecision(ctx context.Context, d aiDecision) bool {
	chat := chatFrom(ctx)
	if chat == nil {
		return false
	}
	if d.Command == "" {
		h.Reply(chat.ChatID, d.hintOr("未能识别该指令，可发 /help 查看命令，或直接发主机名搜索。"))
		return false
	}
	spec := lookupCommand(d.Command)
	if spec == nil || !spec.AI || spec.MinRole > chat.Role {
		h.Reply(chat.ChatID, "该操作不支持通过 AI 触发，请使用对应命令。")
		return false
	}
	h.audit(ctx, "ai", strings.TrimSpace(d.Command+" "+d.Args), "ok")
	h.noteAITurn(chat, d.Command, d.Args)
	h.dispatchCommand(ctx, d.Command, d.Args)
	return true
}

func (h *Hub) noteAITurn(chat *model.BotChat, command, args string) {
	if h == nil || chat == nil || chat.Kind != model.BotChatPrivate {
		return
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	h.aiMu.Lock()
	defer h.aiMu.Unlock()
	if h.aiTurns == nil {
		h.aiTurns = map[int64]aiTurn{}
	}
	h.aiTurns[chat.ChatID] = aiTurn{command: command, args: strings.TrimSpace(args), expire: time.Now().Add(aiTurnTTL)}
}

func (h *Hub) lastAITurn(chatID int64) (string, string, bool) {
	if h == nil {
		return "", "", false
	}
	h.aiMu.Lock()
	defer h.aiMu.Unlock()
	turn, ok := h.aiTurns[chatID]
	if !ok || !turn.expire.After(time.Now()) {
		delete(h.aiTurns, chatID)
		return "", "", false
	}
	return turn.command, turn.args, true
}

func (d aiDecision) hintOr(fallback string) string {
	if strings.TrimSpace(d.Hint) != "" {
		return d.Hint
	}
	return fallback
}

// aiResolve 调用 OpenAI 兼容 chat completions（SSE 流式），要求模型通过工具调用回传指令映射。
// 流式让思考模型的 reasoning 分片持续到达，长耗时下连接不被网关掐断。
// 请求含系统提示、主机名称与分组、上一句命令和用户文本，不携带指标或地址。
func (h *Hub) aiResolve(ctx context.Context, text, prevCmd, prevArgs string) (aiDecision, error) {
	ai := singleton.Conf.Bot.AI
	payload := aiChatRequest{
		Model:       ai.Model,
		Messages:    aiMessages(text, formatHostCatalog(report.AllHosts()), prevCmd, prevArgs),
		Tools:       aiTools(),
		ToolChoice:  "auto",
		Temperature: 0.1,
		// 思考模型的 reasoning 计入补全预算，给足空间；非思考模型到工具调用即停。
		MaxTokens: 4096,
		Stream:    true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return aiDecision{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(ai.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return aiDecision{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if strings.TrimSpace(ai.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(ai.APIKey))
	}
	callCtx, cancel := context.WithTimeout(ctx, aiRequestTimeout)
	defer cancel()
	resp, err := utils.HttpClient.Do(req.WithContext(callCtx))
	if err != nil {
		return aiDecision{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return aiDecision{}, err
	}
	if resp.StatusCode >= 400 {
		return aiDecision{}, aiHTTPError(respBody, resp.StatusCode)
	}
	// 个别服务不理会 stream 标志，直接回整段 JSON；按首字节嗅探分流。
	trimmed := bytes.TrimLeft(respBody, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return parseAIResponse(respBody, resp.StatusCode)
	}
	return parseAIStream(respBody)
}

func aiHTTPError(body []byte, status int) error {
	var out aiChatResponse
	if err := json.Unmarshal(body, &out); err == nil && out.Error != nil && out.Error.Message != "" {
		return fmt.Errorf("ai 接口报错: %s", out.Error.Message)
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", status)
	}
	return fmt.Errorf("%s", msg)
}

// parseAIResponse 从非流式 OpenAI 兼容响应中提取工具调用；纯函数便于测试。
func parseAIResponse(body []byte, status int) (aiDecision, error) {
	var out aiChatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return aiDecision{}, fmt.Errorf("ai 响应解析失败 (HTTP %d)", status)
	}
	if out.Error != nil && out.Error.Message != "" {
		return aiDecision{}, fmt.Errorf("ai 接口报错: %s", out.Error.Message)
	}
	if status >= 400 {
		return aiDecision{}, fmt.Errorf("ai 接口 HTTP %d", status)
	}
	if len(out.Choices) == 0 {
		return aiDecision{}, fmt.Errorf("ai 响应缺少 choices")
	}
	for _, call := range out.Choices[0].Message.ToolCalls {
		if d, ok, err := aiDecisionFromTool(call.Function.Name, call.Function.Arguments); ok {
			return d, err
		}
	}
	// 无工具调用：有文字就当提示语（思考模型常用散文作答），否则视为未命中意图。
	if strings.TrimSpace(out.Choices[0].Message.Content) != "" {
		return aiDecision{Hint: truncateRunes(strings.TrimSpace(out.Choices[0].Message.Content), 160)}, nil
	}
	return aiDecision{}, nil
}

// parseAIStream 解析 SSE 流式响应：增量聚合 content / reasoning_content / tool_calls。
// reasoning_content（思考内容）只做容错吸收，不进决策、不落回复。
func parseAIStream(body []byte) (aiDecision, error) {
	var (
		content   strings.Builder
		toolNames = map[int]string{}
		toolArgs  = map[int]*strings.Builder{}
		streamErr error
	)
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk aiStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			streamErr = fmt.Errorf("ai 接口报错: %s", chunk.Error.Message)
			break
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		content.WriteString(delta.Content)
		for _, tc := range delta.ToolCalls {
			if tc.Function.Name != "" {
				toolNames[tc.Index] = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				if toolArgs[tc.Index] == nil {
					toolArgs[tc.Index] = &strings.Builder{}
				}
				toolArgs[tc.Index].WriteString(tc.Function.Arguments)
			}
		}
	}
	if streamErr != nil {
		return aiDecision{}, streamErr
	}
	// 取编号最小的工具调用。
	if len(toolNames) > 0 {
		minIdx := -1
		for idx := range toolNames {
			if minIdx < 0 || idx < minIdx {
				minIdx = idx
			}
		}
		args := ""
		if toolArgs[minIdx] != nil {
			args = toolArgs[minIdx].String()
		}
		if d, ok, err := aiDecisionFromTool(toolNames[minIdx], args); ok {
			return d, err
		}
	}
	if strings.TrimSpace(content.String()) != "" {
		return aiDecision{Hint: truncateRunes(strings.TrimSpace(content.String()), 160)}, nil
	}
	return aiDecision{}, nil
}

// aiDecisionFromTool 把工具调用还原为决策；返回 false 表示不是 bot_command/noop。
func aiDecisionFromTool(name, arguments string) (aiDecision, bool, error) {
	switch name {
	case "bot_command":
		var args struct {
			Command string `json:"command"`
			Args    string `json:"args"`
		}
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return aiDecision{}, true, fmt.Errorf("ai 工具参数解析失败")
		}
		args.Command = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args.Command), "/")))
		return aiDecision{Command: args.Command, Args: strings.TrimSpace(args.Args)}, true, nil
	case "noop":
		var hint struct {
			Reply string `json:"reply"`
		}
		_ = json.Unmarshal([]byte(arguments), &hint)
		return aiDecision{Hint: hint.Reply}, true, nil
	}
	return aiDecision{}, false, nil
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// TestAI 用给定配置发一次最小 chat completions，返回耗时（毫秒）。
func TestAI(ctx context.Context, conf model.AIConfig) (int64, error) {
	base := strings.TrimSpace(conf.BaseURL)
	target := strings.TrimSpace(conf.Model)
	if base == "" || target == "" {
		return 0, fmt.Errorf("base_url 与模型名不能为空")
	}
	payload := aiChatRequest{
		Model:       target,
		Messages:    []aiMessage{{Role: "user", Content: "ping"}},
		MaxTokens:   1,
		Temperature: 0,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(conf.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(conf.APIKey))
	}
	start := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, aiTestTimeout)
	defer cancel()
	resp, err := utils.HttpClient.Do(req.WithContext(callCtx))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return 0, fmt.Errorf("%s", msg)
	}
	return time.Since(start).Milliseconds(), nil
}

func aiTools() []aiTool {
	return []aiTool{
		{
			Type: "function",
			Function: aiFunctionDefinition{
				Name:        "bot_command",
				Description: "把用户的监控查询意图映射为一条 Bot 命令并执行",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"command": {"type": "string", "description": "命令名，必须原样取自指令目录"},
						"args": {"type": "string", "description": "命令参数，遵循指令目录中的语法，不带前导斜杠"}
					},
					"required": ["command"]
				}`),
			},
		},
		{
			Type: "function",
			Function: aiFunctionDefinition{
				Name:        "noop",
				Description: "用户的意图与监控查询无关或无法识别时调用",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"reply": {"type": "string", "description": "给用户的简短中文说明，不超过 40 字"}
					}
				}`),
			},
		},
	}
}

func formatHostCatalog(hosts []report.HostRow) string {
	if len(hosts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("主机名录（仅名称与分组，无指标）：\n")
	n := len(hosts)
	if n > aiCatalogLimit {
		n = aiCatalogLimit
	}
	for i := 0; i < n; i++ {
		host := hosts[i]
		name := oneLine(host.Name)
		if name == "" {
			continue
		}
		if tag := oneLine(host.Tag); tag != "" {
			fmt.Fprintf(&b, "- %s tag=%s\n", name, tag)
		} else {
			fmt.Fprintf(&b, "- %s\n", name)
		}
	}
	if len(hosts) > aiCatalogLimit {
		fmt.Fprintf(&b, "共 %d 台，未列全。不确定时用 tag= 或 name~。\n", len(hosts))
	}
	return b.String()
}

func oneLine(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func aiMessages(text, catalog, prevCmd, prevArgs string) []aiMessage {
	system := aiSystemPrompt()
	if strings.TrimSpace(catalog) != "" {
		system += "\n" + catalog
	}
	msgs := []aiMessage{{Role: "system", Content: system}}
	if cmd := strings.TrimSpace(prevCmd); cmd != "" {
		line := "/" + cmd
		if args := strings.TrimSpace(prevArgs); args != "" {
			line += " " + args
		}
		msgs = append(msgs,
			aiMessage{Role: "user", Content: "上一句已执行：" + line},
			aiMessage{Role: "assistant", Content: "已执行。"},
		)
	}
	msgs = append(msgs, aiMessage{Role: "user", Content: text})
	return msgs
}

func aiSystemPrompt() string {
	var b strings.Builder
	b.WriteString("你是三太子监控 Telegram Bot 的意图解析器。把用户的一句话转换为至多一条监控查询命令：")
	b.WriteString("只能映射下方目录中列出的只读查询命令，禁止构造写操作或目录外命令。")
	b.WriteString("args 遵循目录中的参数语法；范围只能用 today/yesterday/month/24h/Nd/Nh 或 2026-01-02 形式。")
	b.WriteString("无法识别或超出目录时调用 noop。不要编造主机名，不确定主机时优先用 tag 或 name~ 之类宽松条件。")
	b.WriteString("若用户承接上一句（那、再、改成），参照上一句命令只改参数，仍只输出一条命令。上一句不含查询结果。\n\n指令目录：\n")
	for i := range commandSpecs {
		spec := &commandSpecs[i]
		if !spec.AI {
			continue
		}
		b.WriteString("/")
		b.WriteString(spec.Name)
		if spec.Args != "" {
			b.WriteString(" ")
			b.WriteString(spec.Args)
		}
		b.WriteString(" — ")
		b.WriteString(spec.Desc)
		b.WriteString("\n")
	}
	b.WriteString("\n示例：\"hk 那几台 CPU 最近一小时高不高\" → bot_command {command: \"find\", args: \"tag=hk cpu>0 24h sort=-cpu\"}；\"今天流量用了多少\" → bot_command {command: \"usage\", args: \"today\"}；\"你好\" → noop")
	return b.String()
}
