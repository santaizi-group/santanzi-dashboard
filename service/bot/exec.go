package bot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hi2shark/santaizi-dashboard/service/report"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	trafficservice "github.com/hi2shark/santaizi-dashboard/service/traffic"
)

type evalHost struct {
	Host           report.HostRow
	Stats          report.NodeStats
	Prev           report.NodeStats
	Uptime         report.UptimeRow
	In, Out, Total uint64
	PrevTotal      uint64
	HasStats       bool
	HasTraffic     bool
	MissingBinding bool
}

func (h *Hub) evalQuery(q Query) ([]evalHost, int, error) {
	now := inLoc(q.Range.To)
	if now.IsZero() || q.Range.IsNow() {
		now = inLoc(singletonNow())
	}
	hosts := report.AllHosts()
	filters := resolveHostFilters(hosts, q.Filters)
	q.Filters = filters
	out := make([]evalHost, 0, len(hosts))
	for _, host := range hosts {
		item := evalHost{Host: host}
		if !matchSnapshot(item, filters) {
			continue
		}
		out = append(out, item)
	}
	missing := 0
	needHistory := !q.Range.IsNow() || q.Kind == "usage" || q.Kind == "uptime" || q.Kind == "chart" || q.Compare == "prev"
	if needHistory && singleton.DB != nil {
		bindings, err := report.CurrentBindings(singleton.DB)
		if err != nil {
			return nil, 0, err
		}
		nodes := make([][]byte, 0, len(out))
		for i := range out {
			node := bindings[out[i].Host.ID]
			if len(node) == 0 {
				out[i].MissingBinding = true
				missing++
				continue
			}
			out[i].Host.NodeUUID = node
			nodes = append(nodes, node)
		}
		from, to := q.Range.From, q.Range.To
		if to.IsZero() || from.IsZero() {
			from, to = q.Range.From, now
		}
		if q.needsStatsHistory() {
			stats, err := report.RangeStats(singleton.DB, nodes, from, to)
			if err != nil {
				return nil, missing, err
			}
			for i := range out {
				if len(out[i].Host.NodeUUID) == 0 {
					continue
				}
				item := stats[string(out[i].Host.NodeUUID)]
				out[i].Stats = item
				out[i].HasStats = item.Samples > 0
				out[i].In, out[i].Out, out[i].Total = item.In, item.Out, item.Total
				out[i].HasTraffic = item.Total > 0 || !out[i].MissingBinding
				applyAgg(&out[i], q.Agg)
			}
		} else if !q.Range.IsNow() || q.Kind == "usage" {
			bytes, err := trafficservice.RangeUsageMany(singleton.DB, nodes, from, to)
			if err != nil {
				return nil, missing, err
			}
			for i := range out {
				if len(out[i].Host.NodeUUID) == 0 {
					continue
				}
				item := bytes[string(out[i].Host.NodeUUID)]
				out[i].In, out[i].Out, out[i].Total = item.In, item.Out, item.Total
				out[i].HasTraffic = true
			}
		}
		if q.Kind == "uptime" || q.Kind == "cmp" || q.Kind == "find" || hasField(q, "uptime") || strings.TrimPrefix(q.Sort, "-") == "uptime" {
			uptime := report.CollectUptime(hostsOf(out), from, to)
			byID := map[uint64]report.UptimeRow{}
			for _, row := range uptime {
				byID[row.ServerID] = row
			}
			for i := range out {
				out[i].Uptime = byID[out[i].Host.ID]
			}
		}
		if q.Compare == "prev" && !q.Range.IsNow() {
			span := to.Sub(from)
			prevFrom, prevTo := from.Add(-span), from
			if q.needsStatsHistory() {
				prev, err := report.RangeStats(singleton.DB, nodes, prevFrom, prevTo)
				if err != nil {
					return nil, missing, err
				}
				for i := range out {
					if len(out[i].Host.NodeUUID) == 0 {
						continue
					}
					out[i].Prev = prev[string(out[i].Host.NodeUUID)]
					out[i].PrevTotal = out[i].Prev.Total
				}
			} else {
				prev, err := trafficservice.RangeUsageMany(singleton.DB, nodes, prevFrom, prevTo)
				if err != nil {
					return nil, missing, err
				}
				for i := range out {
					if len(out[i].Host.NodeUUID) == 0 {
						continue
					}
					out[i].PrevTotal = prev[string(out[i].Host.NodeUUID)].Total
				}
			}
		}
	}
	filtered := out[:0]
	for _, item := range out {
		if matchHistory(item, q) {
			filtered = append(filtered, item)
		}
	}
	sortEval(filtered, q)
	return filtered, missing, nil
}

func hostsOf(items []evalHost) []report.HostRow {
	out := make([]report.HostRow, 0, len(items))
	for _, item := range items {
		out = append(out, item.Host)
	}
	return out
}

func applyAgg(item *evalHost, agg string) {
	if item == nil || !item.HasStats {
		return
	}
	switch agg {
	case AggMin:
		item.Host.CPU = item.Stats.CPUMin
		item.Host.MemPct = pctOf(item.Stats.MemUsedMin, item.Host.MemTotal)
		item.Host.DiskPct = pctOf(item.Stats.DiskUsedMin, item.Host.DiskTotal)
		item.Host.Load1 = item.Stats.LoadMin
	case AggAvg:
		item.Host.CPU = item.Stats.CPUAvg
		item.Host.MemPct = pctOf(item.Stats.MemUsedAvg, item.Host.MemTotal)
		item.Host.DiskPct = pctOf(item.Stats.DiskUsedAvg, item.Host.DiskTotal)
		item.Host.Load1 = item.Stats.LoadAvg
	default:
		item.Host.CPU = item.Stats.CPUMax
		item.Host.MemPct = pctOf(item.Stats.MemUsedMax, item.Host.MemTotal)
		item.Host.DiskPct = pctOf(item.Stats.DiskUsedMax, item.Host.DiskTotal)
		item.Host.Load1 = item.Stats.LoadMax
	}
	item.Host.NetInSpeed = item.Stats.NetInSpeedAvg
	item.Host.NetOutSpeed = item.Stats.NetOutSpeedAvg
}

func pctOf(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) * 100 / float64(total)
}

func resolveHostFilters(hosts []report.HostRow, filters []Filter) []Filter {
	out := make([]Filter, 0, len(filters))
	for _, filter := range filters {
		if filter.Field != "host" || filter.Op != "auto" {
			out = append(out, filter)
			continue
		}
		for _, value := range filter.Values {
			out = append(out, resolveAutoHost(hosts, value))
		}
	}
	return out
}

func resolveAutoHost(hosts []report.HostRow, value string) Filter {
	for _, host := range hosts {
		if fmt.Sprintf("%d", host.ID) == value {
			return Filter{Field: "id", Op: "=", Values: []string{value}}
		}
	}
	for _, host := range hosts {
		if strings.EqualFold(host.Name, value) {
			return Filter{Field: "name", Op: "=", Values: []string{host.Name}}
		}
	}
	for _, host := range hosts {
		if strings.EqualFold(hostTag(host), value) {
			return Filter{Field: "tag", Op: "=", Values: []string{hostTag(host)}}
		}
	}
	return Filter{Field: "host", Op: "~", Values: []string{value}}
}

func hostTag(host report.HostRow) string {
	if host.Tag == "" {
		return "default"
	}
	return host.Tag
}

func matchSnapshot(item evalHost, filters []Filter) bool {
	for _, filter := range filters {
		switch filter.Field {
		case "tag":
			if !matchAnyFold(hostTag(item.Host), filter.Values) {
				return false
			}
		case "name":
			ok := false
			for _, value := range filter.Values {
				if filter.Op == "=" && strings.EqualFold(item.Host.Name, value) {
					ok = true
				}
				if filter.Op == "~" && strings.Contains(strings.ToLower(item.Host.Name), strings.ToLower(value)) {
					ok = true
				}
			}
			if !ok {
				return false
			}
		case "host":
			if !matchHostLoose(item.Host, filter.Values) {
				return false
			}
		case "id":
			id := fmt.Sprintf("%d", item.Host.ID)
			if !matchAnyFold(id, filter.Values) {
				return false
			}
		case "online":
			if !item.Host.Online {
				return false
			}
		case "offline":
			if item.Host.Online {
				return false
			}
		case "muted":
			if !singleton.IsBotMuted(item.Host.ID) {
				return false
			}
		case "ver":
			if !matchVersion(item.Host.AgentVersion, filter.Op, first(filter.Values)) {
				return false
			}
		}
	}
	return true
}

func matchHistory(item evalHost, q Query) bool {
	for _, filter := range q.Filters {
		var value float64
		switch filter.Field {
		case "cpu":
			value = item.Host.CPU
		case "mem":
			value = item.Host.MemPct
		case "disk":
			value = item.Host.DiskPct
		case "load":
			value = item.Host.Load1
		case "net":
			value = float64(item.Host.NetInSpeed + item.Host.NetOutSpeed)
		case "total":
			value = float64(item.Total)
		case "uptime":
			value = item.Uptime.Percent
		default:
			continue
		}
		if filter.Op == "metric" {
			continue
		}
		if !compareNum(value, filter.Op, filter.Number) {
			return false
		}
	}
	return true
}

func compareNum(value float64, op string, bound float64) bool {
	switch op {
	case ">":
		return value > bound
	case ">=":
		return value >= bound
	case "<":
		return value < bound
	case "<=":
		return value <= bound
	case "=", "==":
		return value == bound
	case "!=":
		return value != bound
	default:
		return true
	}
}

func matchHostLoose(host report.HostRow, values []string) bool {
	name := strings.ToLower(host.Name)
	tag := strings.ToLower(hostTag(host))
	for _, value := range values {
		v := strings.ToLower(value)
		if strings.Contains(name, v) || strings.Contains(tag, v) {
			return true
		}
	}
	return false
}

func matchAnyFold(got string, values []string) bool {
	for _, value := range values {
		if strings.EqualFold(got, value) {
			return true
		}
	}
	return false
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func matchVersion(have, op, want string) bool {
	cmp := compareVer(have, want)
	return compareNum(float64(cmp), op, 0)
}

func compareVer(a, b string) int {
	as := verParts(a)
	bs := verParts(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func verParts(value string) []int {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "v")
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '.' || r == '-' || r == '+'
	})
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

func sortEval(items []evalHost, q Query) {
	key := q.Sort
	if key == "" && q.Metric != "" {
		key = "-" + q.Metric
	}
	desc := strings.HasPrefix(key, "-")
	key = strings.TrimPrefix(key, "-")
	if key == "" {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		vi, vj := metricValue(items[i], key), metricValue(items[j], key)
		if key == "name" {
			if desc {
				return items[i].Host.Name > items[j].Host.Name
			}
			return items[i].Host.Name < items[j].Host.Name
		}
		if vi == vj {
			return items[i].Host.Name < items[j].Host.Name
		}
		if desc {
			return vi > vj
		}
		return vi < vj
	})
}

func metricValue(item evalHost, key string) float64 {
	switch key {
	case "cpu":
		return item.Host.CPU
	case "mem":
		return item.Host.MemPct
	case "disk":
		return item.Host.DiskPct
	case "load":
		return item.Host.Load1
	case "net":
		return float64(item.Host.NetInSpeed + item.Host.NetOutSpeed)
	case "total":
		return float64(item.Total)
	case "uptime":
		if item.Uptime.Percent != 0 {
			return item.Uptime.Percent
		}
		return 100
	}
	return 0
}

func hasField(q Query, field string) bool {
	for _, filter := range q.Filters {
		if filter.Field == field {
			return true
		}
	}
	return false
}

func singletonNow() time.Time {
	now := time.Now()
	if singleton.Loc != nil {
		return now.In(singleton.Loc)
	}
	return now
}

func groupByTag(items []evalHost) []groupRow {
	index := map[string]*groupRow{}
	var order []string
	for _, item := range items {
		tag := item.Host.Tag
		if tag == "" {
			tag = "default"
		}
		row := index[tag]
		if row == nil {
			row = &groupRow{Tag: tag}
			index[tag] = row
			order = append(order, tag)
		}
		row.Count++
		if item.Host.Online {
			row.Online++
		}
		row.CPU += item.Host.CPU
		row.Mem += item.Host.MemPct
		row.Disk += item.Host.DiskPct
		row.Total += item.Total
		row.Uptime += item.Uptime.Percent
	}
	out := make([]groupRow, 0, len(order))
	for _, tag := range order {
		row := index[tag]
		if row.Count > 0 {
			n := float64(row.Count)
			row.CPU /= n
			row.Mem /= n
			row.Disk /= n
			if row.Uptime > 0 {
				row.Uptime /= n
			}
		}
		out = append(out, *row)
	}
	return out
}

type groupRow struct {
	Tag            string
	Count, Online  int
	CPU, Mem, Disk float64
	Total          uint64
	Uptime         float64
}

func pageSlice[T any](items []T, page, size int) ([]T, int, int) {
	if size <= 0 {
		size = 6
	}
	total := (len(items) + size - 1) / size
	if total == 0 {
		total = 1
	}
	if page < 1 {
		page = 1
	}
	if page > total {
		page = total
	}
	start := (page - 1) * size
	end := start + size
	if start > len(items) {
		start = len(items)
	}
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], page, total
}

func deltaLabel(cur, prev uint64, samples uint32) string {
	if samples == 0 && prev == 0 {
		return "无对照"
	}
	if prev == 0 {
		return "无对照"
	}
	diff := float64(int64(cur) - int64(prev))
	pct := diff * 100 / float64(prev)
	sign := "+"
	if pct < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.0f%%", sign, pct)
}
