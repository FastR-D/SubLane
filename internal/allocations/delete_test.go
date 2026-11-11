package allocations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/storage/db"
)

func TestDeleteRemovesAllowanceAfterInFlightRequestsSettle(t *testing.T) {
	s, conn, user, account := fixture(t)
	ctx := context.Background()
	now := time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	scheme, err := s.SaveScheme(ctx, 0, SchemeInput{Name: "Synthetic allowance", GroupID: 2, Enabled: true, Config: Config{Mode: "tokens", Period: "day", Members: []Share{{UserID: user, Limit: 100}}}})
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(conn)
	if _, err := conn.Exec(`INSERT INTO api_keys(id,user_id,group_id,name,prefix,token_hash,created_at) VALUES(51,?,2,'synthetic','sk-syn',randomblob(32),1)`, user); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO allocation_keys(key_id,scheme_id) VALUES(51,?)`, scheme.ID); err != nil {
		t.Fatal(err)
	}
	r := Request{ID: "synthetic-delete", SchemeID: scheme.ID, UserID: user, GroupID: 2, AccountID: account, Model: "synthetic", StartedAt: now.Unix()}
	if err := Begin(ctx, q, r, now.Unix(), time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, scheme.ID); !errors.Is(err, ErrInFlight) {
		t.Fatalf("in-flight request did not block deletion: %v", err)
	}
	if err := Finish(ctx, q, r.ID, Completion{Input: 42, Known: true, Dispatched: true}, now.UnixMilli(), false); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Detail(ctx, scheme.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted allowance remains readable: %v", err)
	}
	var schemes, revisions, entries, bindings, keys int
	if err := conn.QueryRow(`SELECT (SELECT count(*) FROM allocation_schemes),(SELECT count(*) FROM allocation_revisions),
		(SELECT count(*) FROM allocation_entries),(SELECT count(*) FROM allocation_keys),(SELECT count(*) FROM api_keys WHERE id=51 AND group_id=2)`).Scan(&schemes, &revisions, &entries, &bindings, &keys); err != nil {
		t.Fatal(err)
	}
	if schemes != 0 || revisions != 0 || entries != 0 || bindings != 0 || keys != 1 {
		t.Fatalf("schemes=%d revisions=%d entries=%d bindings=%d keys=%d", schemes, revisions, entries, bindings, keys)
	}
	// The bound key falls back to its group without an allowance.
	if scheme, err := KeyScheme(ctx, q, 51, user, 2, now.Unix()); err != nil || scheme != 0 {
		t.Fatalf("key allowance after deletion: %d %v", scheme, err)
	}
	// The pool is no longer locked, so its group can be deleted or edited freely.
	if err := groups.New(conn).Delete(ctx, 2); !errors.Is(err, groups.ErrInUse) {
		t.Fatalf("group lock was not released: %v", err)
	}
	if err := s.Delete(ctx, scheme.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeated deletion: %v", err)
	}
}
