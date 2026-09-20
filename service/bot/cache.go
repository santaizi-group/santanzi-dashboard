package bot

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	queryCacheTTL    = 30 * time.Minute
	queryCacheLimit  = 512
	queryTokenLength = 3
)

type cachedQuery struct {
	token  string
	chatID int64
	query  Query
	expire time.Time
}

type queryCache struct {
	mu      sync.Mutex
	items   map[string]*cachedQuery
	order   []string
	nowFunc func() time.Time
}

func newQueryCache() *queryCache {
	return &queryCache{items: map[string]*cachedQuery{}, nowFunc: time.Now}
}

func (c *queryCache) Put(chatID int64, q Query) string {
	if c == nil {
		return ""
	}
	token := randomToken()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictLocked()
	for c.items[token] != nil {
		token = randomToken()
	}
	c.items[token] = &cachedQuery{token: token, chatID: chatID, query: q.Clone(), expire: c.now().Add(queryCacheTTL)}
	c.order = append(c.order, token)
	c.shrinkLocked()
	return token
}

func (c *queryCache) Get(token string, chatID int64) (Query, bool) {
	if c == nil || token == "" {
		return Query{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictLocked()
	item := c.items[token]
	if item == nil || item.chatID != chatID {
		return Query{}, false
	}
	return item.query.Clone(), true
}

func (c *queryCache) Patch(token string, chatID int64, overlay string) (Query, string, bool) {
	q, ok := c.Get(token, chatID)
	if !ok {
		return Query{}, "", false
	}
	q = applyOverlay(q, overlay)
	q.Page = max(q.Page, 1)
	newToken := c.Put(chatID, q)
	return q, newToken, true
}

func (c *queryCache) now() time.Time {
	if c.nowFunc != nil {
		return c.nowFunc()
	}
	return time.Now()
}

func (c *queryCache) evictLocked() {
	now := c.now()
	kept := c.order[:0]
	for _, token := range c.order {
		item := c.items[token]
		if item == nil || !item.expire.After(now) {
			delete(c.items, token)
			continue
		}
		kept = append(kept, token)
	}
	c.order = kept
}

func (c *queryCache) shrinkLocked() {
	for len(c.order) > queryCacheLimit {
		old := c.order[0]
		c.order = c.order[1:]
		delete(c.items, old)
	}
}

func randomToken() string {
	var buf [queryTokenLength]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return hex.EncodeToString([]byte{1, 2, 3})
	}
	return hex.EncodeToString(buf[:])
}
