package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

func classifyBotAPIError(err error) error {
	if err == nil {
		return nil
	}
	if message, ok := classifiedBotAPIMessage(err); ok {
		return fmt.Errorf("%s", message)
	}
	return err
}

func classifiedBotAPIMessage(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return unreachableBotAPIMessage, true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return unreachableBotAPIMessage, true
	}
	if errors.Is(err, io.EOF) {
		return unreachableBotAPIMessage, true
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "token 为空") || strings.Contains(msg, "empty token"):
		return "尚未保存 Token。", true
	case strings.Contains(msg, "unauthorized"):
		return "Token 无效。", true
	case strings.Contains(msg, "forbidden"):
		return "Bot 被停用或 Token 无权访问。", true
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "deadline exceeded"),
		strings.Contains(msg, "i/o timeout"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no such host"),
		strings.Contains(msg, "network is unreachable"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "tls handshake timeout"),
		strings.Contains(msg, "stopped after"):
		return unreachableBotAPIMessage, true
	default:
		return "", false
	}
}

const unreachableBotAPIMessage = "无法连接 Telegram Bot API。官方地址不通时，在「Bot API 地址」填写可用的反代根地址。"
