package ws

import (
	"sync"
	"time"
)

const limitWindow = time.Minute

type Limits struct {
	HandshakesPerMinutePerAddress int
	ConnectionsPerAddress         int
	ConnectionsPerSubject         int
	Connections                   int
	FramesPerSecond               float64
	FrameBurst                    float64
}

func DefaultLimits() Limits {
	return Limits{
		HandshakesPerMinutePerAddress: 60,
		ConnectionsPerAddress:         50,
		ConnectionsPerSubject:         10,
		Connections:                   20_000,
		FramesPerSecond:               50,
		FrameBurst:                    100,
	}
}

type limiter struct {
	limits      Limits
	mu          sync.Mutex
	windowStart time.Time
	handshakes  map[string]int
	byAddress   map[string]int
	bySubject   map[string]int
	total       int
}

func newLimiter(limits Limits) *limiter {
	return &limiter{
		limits:     limits,
		handshakes: map[string]int{},
		byAddress:  map[string]int{},
		bySubject:  map[string]int{},
	}
}

func (l *limiter) admit(address string, now time.Time) (release func(), allowed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.windowStart) >= limitWindow {
		l.windowStart = now
		clear(l.handshakes)
	}

	if l.total >= l.limits.Connections ||
		l.byAddress[address] >= l.limits.ConnectionsPerAddress ||
		l.handshakes[address] >= l.limits.HandshakesPerMinutePerAddress {
		return nil, false
	}

	l.handshakes[address]++
	l.byAddress[address]++
	l.total++

	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()

		l.total--
		decrement(l.byAddress, address)
	}, true
}

func (l *limiter) admitSubject(subject string) (release func(), allowed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.bySubject[subject] >= l.limits.ConnectionsPerSubject {
		return nil, false
	}

	l.bySubject[subject]++

	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()

		decrement(l.bySubject, subject)
	}, true
}

func decrement(counts map[string]int, key string) {
	counts[key]--
	if counts[key] <= 0 {
		delete(counts, key)
	}
}

type frameRate struct {
	perSecond float64
	burst     float64
	tokens    float64
	last      time.Time
}

func (l *limiter) newFrameRate(now time.Time) *frameRate {
	return &frameRate{
		perSecond: l.limits.FramesPerSecond,
		burst:     l.limits.FrameBurst,
		tokens:    l.limits.FrameBurst,
		last:      now,
	}
}

func (r *frameRate) allow(now time.Time) bool {
	r.tokens = min(r.burst, r.tokens+now.Sub(r.last).Seconds()*r.perSecond)
	r.last = now
	if r.tokens < 1 {
		return false
	}

	r.tokens--

	return true
}
