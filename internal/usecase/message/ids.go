package message

import (
	"sync"
	"time"
)

const (
	idEpochMillis  = 1_767_225_600_000
	idNodeBits     = 10
	idSequenceBits = 12
	idMaxSequence  = 1<<idSequenceBits - 1
)

type IDs struct {
	mu       sync.Mutex
	node     int64
	now      func() time.Time
	last     int64
	sequence int64
}

func NewIDs(node int64, now func() time.Time) *IDs {
	return &IDs{node: node & (1<<idNodeBits - 1), now: now}
}

func (g *IDs) Next() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()

	millis := g.now().UnixMilli() - idEpochMillis
	if millis > g.last {
		g.last = millis
		g.sequence = 0
	} else if g.sequence < idMaxSequence {
		g.sequence++
	} else {
		g.last++
		g.sequence = 0
	}

	return g.last<<(idNodeBits+idSequenceBits) | g.node<<idSequenceBits | g.sequence
}
