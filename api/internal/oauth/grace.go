package oauth

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// rotation is what one refresh produced: the new pair, and the grant it
// belongs to, so a repeat is refused once that grant is revoked.
type rotation struct {
	pair    TokenPair
	grantID uuid.UUID
}

// graceCache keeps, for refreshGrace after a refresh token is consumed, the
// pair it was rotated into, keyed by rotationKey: the consumed token's hash
// and the scopes its access token carries.
//
// It exists because claude.ai can send the same refresh several times at once
// (anthropics/claude-ai-mcp#1194). Strict rotation would honour the first and
// treat the rest as a stolen token being replayed, revoking the connection the
// person just made. Within the window each repeat gets the pair the first one
// got, so whichever response the client keeps, it keeps a working pair.
//
// The pair is in plain text, so it lives only here, in this process's memory,
// for a minute — never in the database, where only hashes go. Behind more
// than one replica a repeat could reach a process without it; it then gets
// invalid_grant without anything being revoked, and the client still has the
// pair the first response carried.
type graceCache struct {
	mu      sync.Mutex
	entries map[string]graceEntry
}

type graceEntry struct {
	rotation
	until time.Time
}

func newGraceCache() *graceCache { return &graceCache{entries: map[string]graceEntry{}} }

// get returns the rotation with this key, while it is still within its
// window.
func (c *graceCache) get(key string, now time.Time) (rotation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !now.Before(e.until) {
		return rotation{}, false
	}
	return e.rotation, true
}

// put remembers a rotation until the window closes, and drops whatever else
// already closed: the map holds at most a minute of refreshes.
func (c *graceCache) put(key string, r rotation, until, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked(now)
	c.entries[key] = graceEntry{rotation: r, until: until}
}

// sweep drops the entries whose window has closed.
func (c *graceCache) sweep(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked(now)
}

func (c *graceCache) sweepLocked(now time.Time) {
	for key, e := range c.entries {
		if !now.Before(e.until) {
			delete(c.entries, key)
		}
	}
}
