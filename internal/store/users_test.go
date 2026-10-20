package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUsersAndLock(t *testing.T) {
	st, _ := setup(t)
	ctx := context.Background()
	alice, bob := st.For("alice"), st.For("bob")
	if alice.User() != "alice" || st.User() != "tester" || st.For("") != st {
		t.Fatalf("For: %q %q", alice.User(), st.User())
	}
	if err := alice.Lock(ctx, "demo", "host1", time.Minute); err != nil {
		t.Fatal(err)
	}
	// another server of the same user is refused; another user is not affected
	var le *LockedError
	if err := alice.Lock(ctx, "demo", "host2", time.Minute); !errors.As(err, &le) || le.Owner != "host1" {
		t.Fatalf("second lock: %v", err)
	}
	if err := bob.Lock(ctx, "demo", "host2", time.Minute); err != nil {
		t.Fatalf("other user: %v", err)
	}
	// the holder extends; a stranger's unlock does nothing; the holder's unlock frees it
	if err := alice.Lock(ctx, "demo", "host1", time.Minute); err != nil {
		t.Fatal(err)
	}
	alice.Unlock(ctx, "demo", "host2")
	if err := alice.Lock(ctx, "demo", "host2", time.Minute); err == nil {
		t.Fatal("unlocked by a stranger")
	}
	if err := alice.Unlock(ctx, "demo", "host1"); err != nil {
		t.Fatal(err)
	}
	if err := alice.Lock(ctx, "demo", "host2", time.Millisecond); err != nil {
		t.Fatalf("after unlock: %v", err)
	}
	// an expired claim is taken over
	time.Sleep(10 * time.Millisecond)
	if err := alice.Lock(ctx, "demo", "host3", time.Minute); err != nil {
		t.Fatalf("expired: %v", err)
	}
	if h, _ := alice.SaveHistory(ctx, "demo"); len(h) != 0 {
		t.Fatalf("lock listed as a save: %v", h)
	}
}
