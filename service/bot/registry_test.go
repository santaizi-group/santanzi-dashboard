package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/hi2shark/santaizi-dashboard/model"
)

// 旧 dispatch switch 的全部命令词必须仍在注册表里，且角色不变。
func TestRegistryCoversLegacyRouting(t *testing.T) {
	roles := map[string]uint8{
		"start": 0, "bind": 0,
		"mute": model.BotRoleOperator, "unmute": model.BotRoleOperator, "rule": model.BotRoleOperator,
		"chats": model.BotRoleAdmin, "role": model.BotRoleAdmin, "revoke": model.BotRoleAdmin,
		"audit": model.BotRoleAdmin, "health": model.BotRoleAdmin,
	}
	for _, name := range []string{
		"start", "bind", "help", "menu", "whoami", "status", "servers", "server",
		"find", "top", "usage", "traffic", "uptime", "groups", "chart", "cmp",
		"services", "rules", "collectors", "agents", "offline", "probes", "alerts",
		"audit", "health", "report", "mute", "unmute", "rule", "chats", "role", "revoke",
		"cpu", "net",
	} {
		spec := lookupCommand(name)
		if spec == nil {
			t.Fatalf("command %q missing from registry", name)
		}
		if want, ok := roles[name]; ok && spec.MinRole != want {
			t.Fatalf("command %q role=%d want %d", name, spec.MinRole, want)
		}
	}
}

func TestCallbackCommandExplicitMapping(t *testing.T) {
	cases := []struct {
		data string
		cmd  string
		ok   bool
	}{
		{"c:mute:5:3600", "mute", true},
		{"c:unmute:5", "unmute", true},
		{"c:rule:3:1", "rule", true},
		{"c:role:42:3", "role", true},
		{"c:revoke:42", "revoke", true},
		{"m:1h:7", "mute", true},
		{"m:status", "status", true},
		{"m:audit", "audit", true},
		{"m:health", "health", true},
		{"m:chats", "chats", true},
		{"m:home", "menu", true},
		{"m:servers:online", "servers", true},
		{"q:ab12cd:", "status", true},
		{"h:5", "status", true},
		{"x:rule", "status", true},
		{"au:1", "", false},
		{"hl:1", "", false},
		{"evil:payload", "", false},
	}
	for _, tc := range cases {
		cmd, ok := callbackCommand(tc.data)
		if ok != tc.ok || cmd != tc.cmd {
			t.Fatalf("callbackCommand(%q) = (%q,%v), want (%q,%v)", tc.data, cmd, ok, tc.cmd, tc.ok)
		}
	}
}

// 观察角色点管理菜单按钮必须被拦，而不是拿到 admin 数据。
func TestMenuCallbackRejectsUnderprivileged(t *testing.T) {
	h := &Hub{sender: &Sender{ch: make(chan outbound, 8)}}
	ctx := context.WithValue(context.Background(), ctxChat, &model.BotChat{ChatID: 42, Role: model.BotRoleViewer})

	h.onMenuCallback(ctx, "m:audit")
	assertReply(t, h, "权限不足。")
}

func TestDispatchCommandResolvesAliases(t *testing.T) {
	h := &Hub{sender: &Sender{ch: make(chan outbound, 8)}}
	ctx := context.WithValue(context.Background(), ctxChat, &model.BotChat{ChatID: 42, Role: model.BotRoleViewer})

	if !h.dispatchCommand(ctx, "whoami", "") {
		t.Fatal("whoami should dispatch")
	}
	if h.dispatchCommand(ctx, "nope", "") {
		t.Fatal("unknown command should not dispatch")
	}

	spec := lookupCommand("chart")
	orig := spec.Handler
	defer func() { spec.Handler = orig }()
	var got string
	spec.Handler = func(h *Hub, ctx context.Context, arg string) { got = arg }
	h.dispatchCommand(ctx, "cpu", "hk-1 24h")
	if got != "hk-1 24h cpu" {
		t.Fatalf("cpu alias arg=%q", got)
	}
}

func TestDefaultCommandsListings(t *testing.T) {
	cmds := defaultCommands()
	seen := map[string]string{}
	for _, c := range cmds {
		seen[c.Command] = c.Description
	}
	for _, name := range []string{"start", "bind", "server", "offline", "report"} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("SetMyCommands missing %q", name)
		}
	}
	for _, name := range []string{"chats", "role", "revoke", "audit", "health", "mute"} {
		if _, ok := seen[name]; ok {
			t.Fatalf("SetMyCommands should not list %q", name)
		}
	}
}

func TestHelpTextFromRegistry(t *testing.T) {
	h := &Hub{sender: &Sender{ch: make(chan outbound, 8)}}
	viewer := context.WithValue(context.Background(), ctxChat, &model.BotChat{ChatID: 42, Role: model.BotRoleViewer})
	h.cmdHelp(viewer, "")
	msg := <-h.sender.ch
	if !strings.Contains(msg.text, "/status 面板总览") || strings.Contains(msg.text, "/chats") {
		t.Fatalf("viewer help wrong: %q", msg.text)
	}
	admin := context.WithValue(context.Background(), ctxChat, &model.BotChat{ChatID: 42, Role: model.BotRoleAdmin})
	h.cmdHelp(admin, "")
	msg = <-h.sender.ch
	if !strings.Contains(msg.text, "/chats") || !strings.Contains(msg.text, "/revoke") {
		t.Fatalf("admin help missing admin commands: %q", msg.text)
	}
}
