package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// User is the owner of the saves this store reads and writes (VNBOX_USER).
func (s *Store) User() string { return s.cfg.User }

// For returns the store acting for another owner of saves: the same bucket and packages,
// the saves under saves/<user>/. An empty user means this store's own.
func (s *Store) For(user string) *Store {
	if user == "" || user == s.cfg.User {
		return s
	}
	c := *s
	c.cfg.User = user
	return &c
}

// lockInfo is saves/<user>/<name>/lock.json: who is playing and until when the claim holds.
type lockInfo struct {
	Owner string    `json:"owner"`
	Until time.Time `json:"until"`
}

// LockedError means the game is being played elsewhere by the same user.
type LockedError struct {
	Owner string
	Until time.Time
}

func (e *LockedError) Error() string {
	return fmt.Sprintf("already playing on %s (until %s unless renewed)", e.Owner, e.Until.Local().Format("15:04"))
}

func (s *Store) lockKey(name string) string { return s.key("saves", s.cfg.User, name, "lock.json") }

// Lock claims the user's save of a game for owner for ttl, so that two servers do not play
// (and save over) the same save at once. Taking it again as the same owner extends it; a
// claim that ran out (a server that died) is taken over.
func (s *Store) Lock(ctx context.Context, name, owner string, ttl time.Duration) error {
	if body, err := s.get(ctx, s.lockKey(name)); err == nil {
		var li lockInfo
		json.NewDecoder(body).Decode(&li)
		body.Close()
		if li.Owner != owner && time.Now().Before(li.Until) {
			return &LockedError{li.Owner, li.Until}
		}
	} else if !IsNotFound(err) {
		return err
	}
	b, _ := json.Marshal(lockInfo{owner, time.Now().Add(ttl).UTC()})
	return s.put(ctx, s.lockKey(name), bytes.NewReader(b))
}

// Unlock releases the claim if owner still holds it.
func (s *Store) Unlock(ctx context.Context, name, owner string) error {
	body, err := s.get(ctx, s.lockKey(name))
	if err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	var li lockInfo
	json.NewDecoder(body).Decode(&li)
	body.Close()
	if li.Owner != owner {
		return nil
	}
	key := s.lockKey(name)
	_, err = s.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
	return err
}
