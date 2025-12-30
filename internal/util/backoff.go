package util

import (
	"math/rand"
	"time"
)

type Backoff struct {
	min time.Duration
	max time.Duration
	cur time.Duration
}

func NewBackoff(min, max time.Duration) *Backoff {
	return &Backoff{min: min, max: max, cur: min}
}

func (b *Backoff) Reset() {
	b.cur = b.min
}

func (b *Backoff) Next() time.Duration {
	d := b.cur
	if b.cur < b.max {
		b.cur *= 2
		if b.cur > b.max {
			b.cur = b.max
		}
	}
	j := 0.75 + rand.Float64()*0.5
	return time.Duration(float64(d) * j)
}
