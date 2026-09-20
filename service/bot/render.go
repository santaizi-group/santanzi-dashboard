package bot

import (
	"html"
	"strconv"
	"strings"
	"unicode/utf8"
)

const telegramTextLimit = 3900

func Escape(s string) string {
	return html.EscapeString(s)
}

func Bold(s string) string {
	return "<b>" + Escape(s) + "</b>"
}

func Code(s string) string {
	return "<code>" + Escape(s) + "</code>"
}

func SplitChunks(text string) []string {
	if utf8.RuneCountInString(text) <= telegramTextLimit {
		return []string{text}
	}
	lines := strings.Split(text, "\n")
	var chunks []string
	var b strings.Builder
	for _, line := range lines {
		if b.Len()+len(line)+1 > telegramTextLimit && b.Len() > 0 {
			chunks = append(chunks, strings.TrimRight(b.String(), "\n"))
			b.Reset()
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if b.Len() > 0 {
		chunks = append(chunks, strings.TrimRight(b.String(), "\n"))
	}
	return chunks
}

func commandName(text string) (cmd, arg string) {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return "", text
	}
	parts := strings.SplitN(text, " ", 2)
	cmd = strings.TrimPrefix(parts[0], "/")
	if i := strings.Index(cmd, "@"); i >= 0 {
		cmd = cmd[:i]
	}
	cmd = strings.ToLower(cmd)
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}
	return cmd, arg
}

func parsePositiveInt(value string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
