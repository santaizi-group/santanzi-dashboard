package bot

import (
	"fmt"
	"strings"

	"github.com/hi2shark/santaizi-dashboard/service/report"
)

func FormatSnapshot(snap report.Snapshot, sections map[string]bool) string {
	var b strings.Builder
	b.WriteString(Bold("三太子监控"))
	b.WriteString("\n")
	b.WriteString(Escape(snap.GeneratedAt.Format("2006-01-02 15:04")))
	b.WriteString("\n")
	if wantSection(sections, "status") {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("主机 %s/%s　从端 %d/%d　探针 %d/%d\n",
			Code(fmt.Sprintf("%d", snap.OnlineServers)), Code(fmt.Sprintf("%d", snap.TotalServers)),
			snap.CollectorsOnline, snap.CollectorsTotal, snap.ProbesOnline, snap.ProbesTotal))
		if snap.Incidents > 0 {
			b.WriteString(fmt.Sprintf("连通异常 %s\n", Code(fmt.Sprintf("%d", snap.Incidents))))
		}
	}
	if wantSection(sections, "servers") {
		b.WriteString("\n")
		b.WriteString(Bold("主机"))
		b.WriteString("\n")
		limit := 15
		for i, host := range snap.Hosts {
			if i >= limit {
				b.WriteString(fmt.Sprintf("…共 %d 台\n", len(snap.Hosts)))
				break
			}
			state := "离线"
			if host.Online {
				state = "在线"
			}
			tag := host.Tag
			if tag == "" {
				tag = "default"
			}
			b.WriteString(fmt.Sprintf("%s %s %s %s CPU %.0f%% MEM %.0f%%\n",
				Code(fmt.Sprintf("%d", host.ID)), Escape(host.Name), Escape(tag), Escape(state), host.CPU, host.MemPct))
		}
		if len(snap.Hosts) == 0 {
			b.WriteString("暂无主机\n")
		}
	}
	if wantSection(sections, "traffic") && len(snap.Traffic) > 0 {
		b.WriteString("\n")
		b.WriteString(Bold("流量"))
		b.WriteString("\n")
		for i, row := range snap.Traffic {
			if i >= 10 {
				break
			}
			b.WriteString(fmt.Sprintf("%s %.0f%% %s/%s\n", Escape(row.Name), row.Percent, report.FormatBytes(row.Used), report.FormatBytes(row.Quota)))
		}
	}
	if wantSection(sections, "uptime") && len(snap.Uptime) > 0 {
		b.WriteString("\n")
		b.WriteString(Bold("可用率"))
		b.WriteString("\n")
		for i, row := range snap.Uptime {
			if i >= 10 {
				break
			}
			b.WriteString(fmt.Sprintf("%s %.2f%% 离线 %s 最长 %s\n", Escape(row.Name), row.Percent, report.FormatDuration(row.OfflineSec), report.FormatDuration(row.LongestSec)))
		}
	}
	if wantSection(sections, "probes") {
		b.WriteString("\n")
		b.WriteString(Bold("探针观察"))
		b.WriteString(fmt.Sprintf("\n在线 %d/%d　可达 %d　异常 %d\n", snap.ProbesOnline, snap.ProbesTotal, snap.ProbePathsUp, snap.ProbePathsDown))
	}
	if wantSection(sections, "alerts") && len(snap.Alerts) > 0 {
		b.WriteString("\n")
		b.WriteString(Bold("连通异常"))
		b.WriteString("\n")
		for i, row := range snap.Alerts {
			if i >= 8 {
				break
			}
			title := row.Title
			if title == "" {
				title = row.Kind
			}
			b.WriteString(fmt.Sprintf("%s %s\n", Escape(title), Escape(row.Detail)))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func FormatHost(host report.HostRow) string {
	state := "离线"
	if host.Online {
		state = "在线"
	}
	tag := host.Tag
	if tag == "" {
		tag = "default"
	}
	sys := strings.TrimSpace(host.Platform + " " + host.PlatformVer)
	if sys == "" {
		sys = "-"
	}
	ver := host.AgentVersion
	if ver == "" {
		ver = "-"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s %s\n分组 %s　%s\n", Bold(host.Name), Code(fmt.Sprintf("#%d", host.ID)), Escape(tag), Escape(state)))
	b.WriteString(fmt.Sprintf("CPU %.0f%%　MEM %.0f%%　磁盘 %.0f%%　负载 %.2f\n", host.CPU, host.MemPct, host.DiskPct, host.Load1))
	b.WriteString(fmt.Sprintf("网速 ↓%s ↑%s\n", report.FormatBytes(host.NetInSpeed)+"/s", report.FormatBytes(host.NetOutSpeed)+"/s"))
	b.WriteString(fmt.Sprintf("流量 ↓%s ↑%s\n", report.FormatBytes(host.NetIn), report.FormatBytes(host.NetOut)))
	b.WriteString(fmt.Sprintf("系统 %s　探针 %s\n", Escape(sys), Escape(ver)))
	if host.Uptime > 0 {
		b.WriteString("在线 " + Escape(report.FormatDuration(host.Uptime)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func FormatServerPage(hosts []report.HostRow, page, size int) (string, int) {
	if size <= 0 {
		size = 6
	}
	pageHosts, page, total := pageSlice(hosts, page, size)
	var b strings.Builder
	b.WriteString(Bold("主机"))
	b.WriteString(fmt.Sprintf(" %d/%d\n", page, total))
	if len(pageHosts) == 0 {
		b.WriteString("暂无主机")
		return b.String(), total
	}
	for _, host := range pageHosts {
		state := "离线"
		if host.Online {
			state = "在线"
		}
		b.WriteString(fmt.Sprintf("%s %s %s CPU %.0f%%\n", Code(fmt.Sprintf("%d", host.ID)), Escape(host.Name), Escape(state), host.CPU))
	}
	return strings.TrimRight(b.String(), "\n"), total
}

func wantSection(sections map[string]bool, name string) bool {
	if len(sections) == 0 {
		return true
	}
	return sections[name]
}
