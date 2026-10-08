package bot

import (
	"context"
	"strings"

	"github.com/hi2shark/santaizi-dashboard/model"
)

// CommandSpec 是 Bot 命令的唯一注册点：路由、SetMyCommands、/help、菜单与
// 角色门槛全部从这里派生，避免多份清单漂移。
type CommandSpec struct {
	Name    string
	Desc    string // SetMyCommands 与 /help 共用
	Args    string // /help 里的参数示意，如 "<id|名称>"
	Aliases []string
	MinRole uint8
	Handler func(h *Hub, ctx context.Context, arg string)
	// 菜单按钮；Menu 为空表示不进菜单。MenuPage: "main" | "more"。
	Menu     string
	MenuArg  string
	MenuPage string
	Hidden   bool // 不进 Telegram 命令面板（SetMyCommands）
	NoHelp   bool // 不进 /help 列表（start/bind 由绑定流程引导）
	AI       bool // 允许 AI 自然语言映射（仅只读查询类）
}

// dispatchCommand 按注册表路由命令；返回是否命中。
func (h *Hub) dispatchCommand(ctx context.Context, cmd, arg string) bool {
	spec := lookupCommand(cmd)
	if spec == nil {
		return false
	}
	if spec.Name == "chart" && (cmd == "cpu" || cmd == "net") {
		fields := append(strings.Fields(arg), cmd)
		arg = strings.Join(fields, " ")
	}
	spec.Handler(h, ctx, arg)
	return true
}

var (
	commandSpecs []CommandSpec
	commandIndex map[string]*CommandSpec
)

// 在 init 里赋值：Handler 闭包引用各 cmdX 方法，部分方法又经菜单回读注册表，
// 包级 var 直接初始化会构成初始化环。
func init() {
	commandSpecs = []CommandSpec{
		{Name: "start", Desc: "开始绑定", MinRole: 0, NoHelp: true,
			Handler: func(h *Hub, ctx context.Context, arg string) {
				chat := chatFrom(ctx)
				if chat == nil {
					return
				}
				h.cmdStart(ctx, chat.ChatID, arg)
			}},
		{Name: "bind", Desc: "绑定授权码", Args: "<绑定码>", MinRole: 0, NoHelp: true,
			Handler: func(h *Hub, ctx context.Context, arg string) {
				chat := chatFrom(ctx)
				if chat == nil {
					return
				}
				h.cmdBind(ctx, chat.ChatID, arg)
			}},
		{Name: "menu", Desc: "主菜单", MinRole: model.BotRoleViewer,
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdMenu(ctx) }},
		{Name: "help", Desc: "命令说明", Args: "[query]", MinRole: model.BotRoleViewer,
			Menu: "帮助", MenuPage: "more",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdHelp(ctx, arg) }},
		{Name: "status", Desc: "面板总览", MinRole: model.BotRoleViewer, AI: true,
			Menu: "总览", MenuPage: "main",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdStatus(ctx) }},
		{Name: "servers", Desc: "主机列表", Args: "[条件]", MinRole: model.BotRoleViewer, AI: true,
			Menu: "主机", MenuPage: "main",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdServers(ctx, arg, 1) }},
		{Name: "server", Desc: "主机详情", Args: "<id|名称>", MinRole: model.BotRoleViewer, AI: true,
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdServer(ctx, arg) }},
		{Name: "find", Desc: "条件筛选", Args: "<条件> [范围]", MinRole: model.BotRoleViewer, AI: true,
			Handler: runKindAdapter("find")},
		{Name: "top", Desc: "排行", Args: "[指标] [范围]", MinRole: model.BotRoleViewer, AI: true,
			Menu: "排行", MenuArg: "cpu", MenuPage: "main",
			Handler: runKindAdapter("top")},
		{Name: "usage", Desc: "流量统计", Args: "[范围] [分组]", MinRole: model.BotRoleViewer, AI: true,
			Menu: "统计", MenuArg: "today", MenuPage: "main",
			Handler: runKindAdapter("usage")},
		{Name: "traffic", Desc: "流量配额", MinRole: model.BotRoleViewer, AI: true,
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdTraffic(ctx) }},
		{Name: "uptime", Desc: "可用率", Args: "[范围]", MinRole: model.BotRoleViewer, AI: true,
			Menu: "可用率", MenuArg: "7d", MenuPage: "main",
			Handler: runKindAdapter("uptime")},
		{Name: "groups", Desc: "分组", MinRole: model.BotRoleViewer, AI: true,
			Menu: "分组", MenuPage: "main",
			Handler: runKindAdapter("groups")},
		{Name: "chart", Desc: "历史曲线", Args: "<主机> [指标] [范围]", Aliases: []string{"cpu", "net"}, MinRole: model.BotRoleViewer, AI: true,
			Handler: runKindAdapter("chart")},
		{Name: "cmp", Desc: "对比", Args: "<A> <B> [范围]", MinRole: model.BotRoleViewer, AI: true,
			Handler: runKindAdapter("cmp")},
		{Name: "services", Desc: "服务监控", MinRole: model.BotRoleViewer, AI: true,
			Menu: "服务监控", MenuPage: "main",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdServices(ctx, 1) }},
		{Name: "rules", Desc: "告警规则", MinRole: model.BotRoleViewer, AI: true,
			Menu: "规则", MenuPage: "more",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdRules(ctx, 1) }},
		{Name: "collectors", Desc: "从端", MinRole: model.BotRoleViewer, AI: true,
			Menu: "从端", MenuPage: "more",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdCollectors(ctx, 1) }},
		{Name: "agents", Desc: "探针版本", MinRole: model.BotRoleViewer, AI: true,
			Menu: "探针版本", MenuPage: "more",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdAgents(ctx, 1) }},
		{Name: "offline", Desc: "离线记录", Args: "<主机>", MinRole: model.BotRoleViewer, AI: true,
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdOffline(ctx, arg) }},
		{Name: "probes", Desc: "探针观察", MinRole: model.BotRoleViewer, AI: true,
			Menu: "探针", MenuPage: "main",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdProbes(ctx) }},
		{Name: "alerts", Desc: "连通异常", MinRole: model.BotRoleViewer, AI: true,
			Menu: "连通异常", MenuPage: "main",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdAlerts(ctx) }},
		{Name: "report", Desc: "立即报告", Args: "now [daily|weekly|monthly]", MinRole: model.BotRoleViewer, AI: true,
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdReport(ctx, arg) }},
		{Name: "whoami", Desc: "当前权限", MinRole: model.BotRoleViewer,
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdWhoami(ctx) }},

		{Name: "mute", Desc: "静音主机告警", Args: "<主机> [时长]", MinRole: model.BotRoleOperator, Hidden: true,
			Handler: muteAdapter(false)},
		{Name: "unmute", Desc: "恢复主机告警", Args: "<主机>", MinRole: model.BotRoleOperator, Hidden: true,
			Handler: muteAdapter(true)},
		{Name: "rule", Desc: "启停告警规则", Args: "<id> on|off", MinRole: model.BotRoleOperator, Hidden: true,
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdRule(ctx, arg, false) }},

		{Name: "chats", Desc: "授权会话", MinRole: model.BotRoleAdmin, Hidden: true,
			Menu: "会话", MenuPage: "more",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdChats(ctx) }},
		{Name: "role", Desc: "调整会话角色", Args: "<chatID> <角色>", MinRole: model.BotRoleAdmin, Hidden: true,
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdRole(ctx, arg) }},
		{Name: "revoke", Desc: "撤销会话", Args: "<chatID>", MinRole: model.BotRoleAdmin, Hidden: true,
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdRevoke(ctx, arg) }},
		{Name: "audit", Desc: "操作日志", MinRole: model.BotRoleAdmin, Hidden: true,
			Menu: "操作日志", MenuPage: "more",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdAudit(ctx, 1) }},
		{Name: "health", Desc: "面板自检", MinRole: model.BotRoleAdmin, Hidden: true,
			Menu: "自检", MenuPage: "more",
			Handler: func(h *Hub, ctx context.Context, arg string) { h.cmdHealth(ctx) }},
	}
	commandIndex = buildCommandIndex()
}

func runKindAdapter(kind string) func(h *Hub, ctx context.Context, arg string) {
	return func(h *Hub, ctx context.Context, arg string) { h.runKind(ctx, kind, arg) }
}

func muteAdapter(unmute bool) func(h *Hub, ctx context.Context, arg string) {
	return func(h *Hub, ctx context.Context, arg string) { h.cmdMute(ctx, arg, unmute) }
}

func buildCommandIndex() map[string]*CommandSpec {
	index := make(map[string]*CommandSpec, len(commandSpecs)*2)
	for i := range commandSpecs {
		spec := &commandSpecs[i]
		index[spec.Name] = spec
		for _, alias := range spec.Aliases {
			index[alias] = spec
		}
	}
	return index
}

func lookupCommand(name string) *CommandSpec {
	return commandIndex[name]
}
