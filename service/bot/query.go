package bot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/hi2shark/santaizi-dashboard/service/report"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
	trafficservice "github.com/hi2shark/santaizi-dashboard/service/traffic"
)

const (
	AggMax = "max"
	AggAvg = "avg"
	AggMin = "min"

	queryUsage = "用法：字段 范围 排序。可用字段 cpu mem disk net total uptime load tag name ver；范围 today yesterday 7d 30d month；排序 sort=-cpu；分组 group=tag。"
)

var (
	errQueryUsage    = errors.New(queryUsage)
	errQueryTooLarge = errors.New("时间范围超过上限。历史筛选最长 30 天，流量统计最长 90 天。")
	errQueryBadRange = errors.New("时间范围无效。")
	errQueryBadToken = errors.New("无法解析该条件。")
)

type Filter struct {
	Field  string
	Op     string
	Values []string
	Number float64
}

type TimeRange struct {
	Kind string
	From time.Time
	To   time.Time
}

func (r TimeRange) IsNow() bool {
	return r.Kind == "" || r.Kind == "now"
}

func (r TimeRange) Label() string {
	switch r.Kind {
	case "", "now":
		return "当前"
	case "today":
		return "今日"
	case "yesterday":
		return "昨日"
	case "24h":
		return "24h"
	case "7d":
		return "7天"
	case "30d":
		return "30天"
	case "month":
		return "本月"
	case "custom":
		if r.From.IsZero() {
			return "自定义"
		}
		return r.From.Format("2006-01-02") + " ~ " + r.To.Format("2006-01-02")
	default:
		if strings.HasSuffix(r.Kind, "d") || strings.HasSuffix(r.Kind, "h") {
			return r.Kind
		}
		return r.Kind
	}
}

type Query struct {
	Kind    string
	Filters []Filter
	Range   TimeRange
	Agg     string
	Sort    string
	GroupBy string
	Compare string
	Limit   int
	Page    int
	Metric  string
	Left    []Filter
	Right   []Filter
}

func (q Query) Clone() Query {
	cp := q
	if q.Filters != nil {
		cp.Filters = append([]Filter(nil), q.Filters...)
	}
	if q.Left != nil {
		cp.Left = append([]Filter(nil), q.Left...)
	}
	if q.Right != nil {
		cp.Right = append([]Filter(nil), q.Right...)
	}
	return cp
}

func ParseQuery(args []string, now time.Time) (Query, error) {
	q := Query{Agg: AggMax, Page: 1, Limit: 8}
	if now.IsZero() {
		now = time.Now()
	}
	now = inLoc(now)
	var pendingAgg string
	var dates []string
	for i := 0; i < len(args); i++ {
		tok := strings.TrimSpace(args[i])
		if tok == "" {
			continue
		}
		lower := strings.ToLower(tok)
		if pendingAgg == "" {
			if lower == AggMax+":" || lower == AggAvg+":" || lower == AggMin+":" {
				pendingAgg = strings.TrimSuffix(lower, ":")
				continue
			}
			if strings.HasPrefix(lower, AggMax+":") || strings.HasPrefix(lower, AggAvg+":") || strings.HasPrefix(lower, AggMin+":") {
				q.Agg = strings.SplitN(lower, ":", 2)[0]
				tok = tok[4:]
				lower = strings.ToLower(tok)
			}
		} else {
			q.Agg = pendingAgg
			pendingAgg = ""
		}
		switch {
		case lower == "today" || lower == "yesterday" || lower == "month" || lower == "24h" || isDaySpan(lower) || isHourSpan(lower):
			rng, err := resolveRange(lower, "", now)
			if err != nil {
				return q, err
			}
			q.Range = rng
		case strings.Contains(tok, ".."):
			left, right, ok := strings.Cut(tok, "..")
			if !ok || !isDate(left) || !isDate(right) {
				return q, errQueryBadRange
			}
			rng, err := resolveRange("custom", left+" "+right, now)
			if err != nil {
				return q, err
			}
			q.Range = rng
		case isDate(tok):
			dates = append(dates, tok)
			if len(dates) == 2 {
				rng, err := resolveRange("custom", dates[0]+" "+dates[1], now)
				if err != nil {
					return q, err
				}
				q.Range = rng
				dates = nil
			}
		case strings.HasPrefix(lower, "sort="):
			q.Sort = strings.TrimPrefix(lower, "sort=")
			if !validSort(q.Sort) {
				return q, fmt.Errorf("%w 排序字段无效。", errQueryBadToken)
			}
		case strings.HasPrefix(lower, "group="):
			q.GroupBy = strings.TrimPrefix(lower, "group=")
			if q.GroupBy != "tag" && q.GroupBy != "" && q.GroupBy != "host" {
				return q, fmt.Errorf("%w 分组只支持 tag。", errQueryBadToken)
			}
			if q.GroupBy == "host" {
				q.GroupBy = ""
			}
		case strings.HasPrefix(lower, "top=") || strings.HasPrefix(lower, "limit="):
			raw := strings.TrimPrefix(strings.TrimPrefix(lower, "top="), "limit=")
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 || n > 50 {
				return q, fmt.Errorf("%w top 取值 1–50。", errQueryBadToken)
			}
			q.Limit = n
		case lower == "vs=prev" || lower == "compare=prev":
			q.Compare = "prev"
		case lower == "online" || lower == "offline" || lower == "muted":
			q.Filters = append(q.Filters, Filter{Field: lower, Op: "="})
		default:
			filter, ok, err := parseFilter(tok)
			if err != nil {
				return q, err
			}
			if !ok {
				return q, fmt.Errorf("%s\n%s", queryUsage, "无法解析："+tok)
			}
			q.Filters = append(q.Filters, filter)
		}
	}
	if pendingAgg != "" {
		q.Agg = pendingAgg
	}
	if len(dates) == 1 {
		return q, errQueryBadRange
	}
	if err := q.checkSpan(); err != nil {
		return q, err
	}
	return q, nil
}

func (q Query) checkSpan() error {
	if q.Range.IsNow() || q.Range.From.IsZero() {
		return nil
	}
	span := q.Range.To.Sub(q.Range.From)
	if span <= 0 {
		return errQueryBadRange
	}
	if q.needsStatsHistory() && span > report.MaxStatsLookback {
		return errQueryTooLarge
	}
	if span > trafficservice.MaxUsageLookback {
		return errQueryTooLarge
	}
	return nil
}

func (q Query) needsStatsHistory() bool {
	if q.Range.IsNow() {
		return false
	}
	if q.Kind == "usage" || q.Kind == "uptime" {
		return false
	}
	if statsField(q.Metric) {
		return true
	}
	if statsField(strings.TrimPrefix(q.Sort, "-")) {
		return true
	}
	for _, filter := range q.Filters {
		if statsField(filter.Field) {
			return true
		}
	}
	return q.Kind == "chart" || q.Kind == "cmp" || q.Kind == "top" || q.Kind == "find" || q.Kind == "groups" || q.Kind == "servers"
}

func statsField(name string) bool {
	switch name {
	case "cpu", "mem", "disk", "load", "net":
		return true
	}
	return false
}

func resolveRange(kind, extra string, now time.Time) (TimeRange, error) {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	switch kind {
	case "today":
		return TimeRange{Kind: "today", From: today, To: now}, nil
	case "yesterday":
		return TimeRange{Kind: "yesterday", From: today.AddDate(0, 0, -1), To: today}, nil
	case "month":
		month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		return TimeRange{Kind: "month", From: month, To: now}, nil
	case "24h":
		return TimeRange{Kind: "24h", From: now.Add(-24 * time.Hour), To: now}, nil
	case "custom":
		parts := strings.Fields(extra)
		if len(parts) != 2 {
			return TimeRange{}, errQueryBadRange
		}
		from, err := time.ParseInLocation("2006-01-02", parts[0], loc)
		if err != nil {
			return TimeRange{}, errQueryBadRange
		}
		toDay, err := time.ParseInLocation("2006-01-02", parts[1], loc)
		if err != nil {
			return TimeRange{}, errQueryBadRange
		}
		if toDay.Before(from) {
			return TimeRange{}, errQueryBadRange
		}
		end := toDay.AddDate(0, 0, 1)
		if !toDay.Before(today) {
			end = now
		}
		return TimeRange{Kind: "custom", From: from, To: end}, nil
	default:
		if n, ok := parseSpan(kind, "d"); ok {
			from := today.AddDate(0, 0, -(n - 1))
			return TimeRange{Kind: kind, From: from, To: now}, nil
		}
		if n, ok := parseSpan(kind, "h"); ok {
			return TimeRange{Kind: kind, From: now.Add(-time.Duration(n) * time.Hour), To: now}, nil
		}
	}
	return TimeRange{}, errQueryBadRange
}

func parseFilter(tok string) (Filter, bool, error) {
	lower := strings.ToLower(tok)
	if strings.HasPrefix(lower, "tag=") {
		raw := tok[4:]
		values := splitOR(raw)
		if len(values) == 0 {
			return Filter{}, false, errQueryBadToken
		}
		return Filter{Field: "tag", Op: "=", Values: values}, true, nil
	}
	if strings.HasPrefix(lower, "name~") || strings.HasPrefix(lower, "name=") {
		op := "~"
		raw := tok[5:]
		if strings.HasPrefix(lower, "name=") {
			op = "="
			raw = tok[5:]
		}
		if strings.TrimSpace(raw) == "" {
			return Filter{}, false, errQueryBadToken
		}
		return Filter{Field: "name", Op: op, Values: splitOR(raw)}, true, nil
	}
	if strings.HasPrefix(lower, "id=") || strings.HasPrefix(lower, "#") {
		raw := strings.TrimPrefix(strings.TrimPrefix(lower, "id="), "#")
		return Filter{Field: "id", Op: "=", Values: splitOR(raw)}, true, nil
	}
	for _, field := range []string{"cpu", "mem", "disk", "net", "total", "uptime", "load"} {
		if !strings.HasPrefix(lower, field) {
			continue
		}
		rest := lower[len(field):]
		op, numRaw, ok := splitOp(rest)
		if !ok {
			if rest == "" {
				return Filter{Field: field, Op: "metric"}, true, nil
			}
			continue
		}
		num, err := parseNumber(numRaw, field)
		if err != nil {
			return Filter{}, false, err
		}
		return Filter{Field: field, Op: op, Number: num}, true, nil
	}
	if strings.HasPrefix(lower, "ver") {
		rest := lower[3:]
		op, numRaw, ok := splitOp(rest)
		if !ok {
			return Filter{}, false, errQueryBadToken
		}
		return Filter{Field: "ver", Op: op, Values: []string{numRaw}}, true, nil
	}
	if isBareName(tok) {
		return Filter{Field: "name", Op: "~", Values: []string{tok}}, true, nil
	}
	return Filter{}, false, nil
}

func splitOp(rest string) (op, value string, ok bool) {
	for _, candidate := range []string{">=", "<=", "!=", ">", "<", "="} {
		if strings.HasPrefix(rest, candidate) {
			return candidate, strings.TrimSpace(rest[len(candidate):]), true
		}
	}
	return "", "", false
}

func parseNumber(raw, field string) (float64, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	mult := 1.0
	if field == "net" || field == "total" {
		switch {
		case strings.HasSuffix(raw, "t"):
			mult = 1024 * 1024 * 1024 * 1024
			raw = strings.TrimSuffix(raw, "t")
		case strings.HasSuffix(raw, "g"):
			mult = 1024 * 1024 * 1024
			raw = strings.TrimSuffix(raw, "g")
		case strings.HasSuffix(raw, "m"):
			mult = 1024 * 1024
			raw = strings.TrimSuffix(raw, "m")
		case strings.HasSuffix(raw, "k"):
			mult = 1024
			raw = strings.TrimSuffix(raw, "k")
		}
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%w 数值无效。", errQueryBadToken)
	}
	return n * mult, nil
}

func splitOR(raw string) []string {
	parts := strings.Split(raw, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func validSort(value string) bool {
	value = strings.TrimPrefix(value, "-")
	switch value {
	case "cpu", "mem", "disk", "net", "total", "uptime", "name", "load":
		return true
	}
	return false
}

func isDaySpan(value string) bool {
	_, ok := parseSpan(value, "d")
	return ok
}

func isHourSpan(value string) bool {
	_, ok := parseSpan(value, "h")
	return ok
}

func parseSpan(value, suffix string) (int, bool) {
	if !strings.HasSuffix(value, suffix) {
		return 0, false
	}
	raw := strings.TrimSuffix(value, suffix)
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || n > 365 {
		return 0, false
	}
	return n, true
}

func isDate(value string) bool {
	if len(value) != 10 {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func isBareName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r == '=' || r == '>' || r == '<' || r == ':' {
			return false
		}
	}
	return unicode.IsLetter([]rune(value)[0]) || strings.ContainsAny(value, "-_.") || unicode.IsDigit([]rune(value)[0])
}

func inLoc(now time.Time) time.Time {
	if singleton.Loc != nil {
		return now.In(singleton.Loc)
	}
	return now
}

func applyOverlay(q Query, overlay string) Query {
	q = q.Clone()
	for _, part := range strings.Split(overlay, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch key {
		case "r":
			rng, err := resolveRange(val, "", inLoc(time.Now()))
			if err == nil {
				q.Range = rng
			}
		case "s":
			if validSort(val) {
				q.Sort = val
			}
		case "g":
			if val == "tag" {
				q.GroupBy = "tag"
			} else {
				q.GroupBy = ""
			}
		case "p":
			n, err := strconv.Atoi(val)
			if err == nil && n > 0 {
				q.Page = n
			}
		case "m":
			q.Metric = val
		}
	}
	return q
}

func defaultRangeFor(kind string) string {
	switch kind {
	case "usage":
		return "today"
	case "uptime":
		return "7d"
	case "chart":
		return "24h"
	default:
		return "now"
	}
}

func parseKindQuery(kind, arg string, now time.Time) (Query, error) {
	fields := strings.Fields(strings.TrimSpace(arg))
	if now.IsZero() {
		now = inLoc(time.Now())
	}
	switch kind {
	case "top":
		metric := "cpu"
		if len(fields) > 0 && isMetricName(fields[0]) {
			metric = strings.ToLower(fields[0])
			fields = fields[1:]
		}
		q, err := ParseQuery(fields, now)
		if err != nil {
			return q, err
		}
		q.Kind = "top"
		q.Metric = metric
		if q.Sort == "" {
			q.Sort = "-" + metric
		}
		if q.Limit == 8 {
			q.Limit = 8
		}
		return q, nil
	case "chart":
		var selector []string
		if len(fields) > 0 {
			selector = []string{fields[0]}
			fields = fields[1:]
		}
		metric := "cpu"
		if len(fields) > 0 && isMetricName(fields[0]) {
			metric = strings.ToLower(fields[0])
			fields = fields[1:]
		}
		q, err := ParseQuery(append(selector, fields...), now)
		if err != nil {
			return q, err
		}
		q.Kind = "chart"
		q.Metric = metric
		if q.Range.IsNow() {
			rng, err := resolveRange("24h", "", now)
			if err != nil {
				return q, err
			}
			q.Range = rng
		}
		return q, nil
	case "cmp":
		leftToks, rest := takeSelector(fields)
		rightToks, rest := takeSelector(rest)
		q, err := ParseQuery(rest, now)
		if err != nil {
			return q, err
		}
		left, err := ParseQuery(leftToks, now)
		if err != nil {
			return q, err
		}
		right, err := ParseQuery(rightToks, now)
		if err != nil {
			return q, err
		}
		q.Kind = "cmp"
		q.Left = left.Filters
		q.Right = right.Filters
		return q, nil
	case "uptime":
		if len(fields) == 1 {
			if n, ok := parsePositiveInt(fields[0]); ok && n > 0 && n <= 90 {
				fields[0] = fmt.Sprintf("%dd", n)
			}
		}
	}
	q, err := ParseQuery(fields, now)
	if err != nil {
		return q, err
	}
	q.Kind = kind
	if q.Range.IsNow() {
		switch kind {
		case "usage":
			rng, err := resolveRange("today", "", now)
			if err != nil {
				return q, err
			}
			q.Range = rng
		case "uptime":
			rng, err := resolveRange("7d", "", now)
			if err != nil {
				return q, err
			}
			q.Range = rng
		}
	}
	if kind == "groups" && q.GroupBy == "" {
		q.GroupBy = "tag"
	}
	if kind == "usage" && q.Sort == "" {
		q.Sort = "-total"
	}
	if kind == "uptime" && q.Sort == "" {
		q.Sort = "uptime"
	}
	return q, nil
}

func takeSelector(fields []string) (sel, rest []string) {
	if len(fields) == 0 {
		return nil, nil
	}
	return fields[:1], fields[1:]
}

func isMetricName(value string) bool {
	switch strings.ToLower(value) {
	case "cpu", "mem", "disk", "net", "total", "uptime", "load":
		return true
	}
	return false
}
