package platform

import (
	"github.com/oklog/ulid/v2"
	"sync"
	"time"
)

type Clock interface{ Now() time.Time }
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type FakeClock struct {
	mu  sync.RWMutex
	now time.Time
}

func NewFakeClock(now time.Time) *FakeClock  { return &FakeClock{now: now} }
func (c *FakeClock) Now() time.Time          { c.mu.RLock(); defer c.mu.RUnlock(); return c.now }
func (c *FakeClock) Set(t time.Time)         { c.mu.Lock(); defer c.mu.Unlock(); c.now = t }
func (c *FakeClock) Advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }
func NewID() string                          { return ulid.Make().String() }
