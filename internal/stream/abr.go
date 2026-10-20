package stream

import "time"

// Adaptive bitrate. Every viewer's browser sends RTCP receiver reports (the fraction of
// packets lost since the last report, about once a second) and, when it estimates the
// bandwidth, REMB. Each viewer gets a rate on a ladder below the configured ceiling: down a
// step after two lossy reports in a row, up a step after 10 seconds without loss. The encoder
// is shared by everyone watching the game, so it runs at the lowest viewer's rate.

// ladder are the rates as fractions of the ceiling (-bitrate, 3000 kbit/s by default).
var ladder = []float64{1, 0.66, 0.4, 0.23, 0.13}

const (
	lossyFraction = 0.08             // a report losing more than this counts as lossy
	cleanFraction = 0.02             // ... and less than this as clean
	upAfter       = 10 * time.Second // clean time before trying the next step up
)

// rateCtl is the rate of one viewer.
type rateCtl struct {
	ceiling    int // kbit/s
	step       int // index into ladder
	lossy      int // lossy reports in a row
	cleanSince time.Time
	remb       int // the browser's own estimate in kbit/s (0 = none)
}

func newRateCtl(ceiling int, now time.Time) *rateCtl {
	return &rateCtl{ceiling: ceiling, cleanSince: now}
}

// loss takes one receiver report (fraction lost, 0..1) and reports whether the rate changed.
func (c *rateCtl) loss(fraction float64, now time.Time) bool {
	switch {
	case fraction > lossyFraction:
		c.lossy++
		c.cleanSince = now
		if c.lossy >= 2 && c.step < len(ladder)-1 {
			c.step++
			c.lossy = 0
			return true
		}
	case fraction < cleanFraction:
		c.lossy = 0
		if c.step > 0 && now.Sub(c.cleanSince) >= upAfter {
			c.step--
			c.cleanSince = now
			return true
		}
	default:
		c.lossy = 0
		c.cleanSince = now
	}
	return false
}

// estimate takes a REMB (bits/s) and reports whether the rate changed.
func (c *rateCtl) estimate(bps float32, now time.Time) bool {
	before := c.kbps()
	c.remb = int(bps / 1000)
	return c.kbps() != before
}

// kbps is the rate for this viewer: its ladder step, never above the browser's estimate
// (with some headroom for audio and packet overhead), never below the lowest step.
func (c *rateCtl) kbps() int {
	r := int(float64(c.ceiling) * ladder[c.step])
	if c.remb > 0 && c.remb*85/100 < r {
		r = c.remb * 85 / 100
	}
	return max(r, int(float64(c.ceiling)*ladder[len(ladder)-1]))
}
