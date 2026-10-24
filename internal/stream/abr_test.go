package stream

import (
	"testing"
	"time"
)

func TestRateCtl(t *testing.T) {
	now := time.Unix(1000, 0)
	c := newRateCtl(3000, now)
	if c.kbps() != 3000 {
		t.Fatalf("start %d", c.kbps())
	}
	// one lossy report is noise; two in a row step down
	if c.loss(0.2, now) || c.kbps() != 3000 {
		t.Fatal("stepped down after one lossy report")
	}
	if !c.loss(0.2, now.Add(time.Second)) || c.kbps() != 1980 {
		t.Fatalf("after two lossy reports: %d", c.kbps())
	}
	// keeps falling while it loses, down to the lowest step
	for i := 0; i < 20; i++ {
		c.loss(0.3, now.Add(time.Duration(2+i)*time.Second))
	}
	if c.kbps() != 390 {
		t.Fatalf("floor %d", c.kbps())
	}
	// clean for 10 s after the last loss → one step up, then another 10 s for the next
	t0 := now.Add(30 * time.Second)
	c.loss(0.3, t0)
	if c.loss(0, t0.Add(5*time.Second)) {
		t.Fatal("stepped up too early")
	}
	if !c.loss(0, t0.Add(11*time.Second)) || c.kbps() != 690 {
		t.Fatalf("after 10 clean seconds: %d", c.kbps())
	}
	if c.loss(0, t0.Add(15*time.Second)) {
		t.Fatal("two steps at once")
	}
	// moderate loss holds the rate and restarts the clean time
	c.loss(0.05, t0.Add(16*time.Second))
	if c.loss(0, t0.Add(22*time.Second)) {
		t.Fatal("stepped up right after moderate loss")
	}
	// the browser's estimate caps the rate
	d := newRateCtl(3000, now)
	if !d.estimate(1_000_000, now) || d.kbps() != 690 {
		t.Fatalf("remb cap %d", d.kbps())
	}
	if d.estimate(1_000_000, now) {
		t.Fatal("same estimate reported as a change")
	}
	// an estimate wobbling between two ladder steps is no change
	if d.estimate(1_100_000, now) || d.estimate(950_000, now) {
		t.Fatal("small estimate change moved the rate")
	}
	d.estimate(100_000, now)
	if d.kbps() != 390 {
		t.Fatalf("remb below the floor: %d", d.kbps())
	}
}
