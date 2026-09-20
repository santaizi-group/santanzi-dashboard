package bot

import (
	"testing"
	"time"
)

func TestQueryCacheExpiresAndRejectsOtherChat(t *testing.T) {
	c := newQueryCache()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	c.nowFunc = func() time.Time { return now }
	token := c.Put(7, Query{Kind: "find", Sort: "-cpu"})
	if token == "" {
		t.Fatal("token")
	}
	got, ok := c.Get(token, 7)
	if !ok || got.Sort != "-cpu" {
		t.Fatalf("got=%#v ok=%v", got, ok)
	}
	if _, ok := c.Get(token, 8); ok {
		t.Fatal("other chat must not read")
	}
	c.nowFunc = func() time.Time { return now.Add(31 * time.Minute) }
	if _, ok := c.Get(token, 7); ok {
		t.Fatal("expired")
	}
}

func TestQueryCachePatchWritesNewToken(t *testing.T) {
	c := newQueryCache()
	token := c.Put(1, Query{Kind: "top", Metric: "cpu", Page: 1})
	q, next, ok := c.Patch(token, 1, "p=2,s=-mem")
	if !ok || next == "" || q.Page != 2 || q.Sort != "-mem" {
		t.Fatalf("q=%#v next=%s", q, next)
	}
}
