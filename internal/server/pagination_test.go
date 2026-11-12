package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/storage/db"
	"github.com/murongg/SubLane/internal/tenants"
	"github.com/murongg/SubLane/internal/testdb"
)

func TestManagementNumberedPagination(t *testing.T) {
	ctx := context.Background()
	connection, err := testdb.Open(ctx, filepath.Join(t.TempDir(), "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	keys := newTestKeyService(t, connection)
	h := New(Options{Auth: identity, Keys: keys, Audit: audit.New(connection), Tenants: tenants.New(connection)})
	setup := request(h, "POST", "/api/auth/setup", "http://example.test", map[string]string{"username": "synthetic-owner", "password": "synthetic-password", "workspace_name": "Synthetic workspace"}, nil)
	if setup.Code != 201 {
		t.Fatal(setup.Code, setup.Body.String())
	}
	cookie := setup.Result().Cookies()[0]
	configureTestPool(t, connection)
	q := db.New(connection)
	for i := int64(0); i < 121; i++ {
		if i > 0 {
			if _, err := connection.ExecContext(ctx, "INSERT INTO users(id,username,role,password_hash,enabled,created_at) VALUES(?,?,'member','synthetic-hash',1,1)", i+1, fmt.Sprintf("synthetic-member-%d", i)); err != nil {
				t.Fatal(err)
			}
			if _, err := connection.ExecContext(ctx, "INSERT INTO memberships(tenant_id,user_id,role,enabled,created_at) VALUES(1,?,'member',1,1)", i+1); err != nil {
				t.Fatal(err)
			}
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("synthetic-key-%d", i)))
		id, err := q.CreateKey(ctx, db.CreateKeyParams{UserID: 1, GroupID: 1, Name: fmt.Sprintf("Synthetic key %d", i), Prefix: "sl_synthetic", TokenHash: hash[:], CreatedAt: 1})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, err := connection.ExecContext(ctx, "UPDATE api_keys SET revoked_at=1 WHERE id=?", id); err != nil {
				t.Fatal(err)
			}
		}
		if err := q.CreateAuditEvent(ctx, db.CreateAuditEventParams{TenantID: 1, ActorID: 1, ActorName: "synthetic-owner", ActorRole: "admin", Source: "user", Action: "key.update", Resource: "key", ResourceID: fmt.Sprint(id), Outcome: "success", CreatedAt: time.Now().Unix()}); err != nil {
			t.Fatal(err)
		}
	}
	// Another owner's key must never contribute to a personal page or its total.
	hash := sha256.Sum256([]byte("synthetic-foreign-key"))
	if _, err := q.CreateKey(ctx, db.CreateKeyParams{UserID: 2, GroupID: 1, Name: "Synthetic foreign key", Prefix: "sl_synthetic", TokenHash: hash[:], CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, field             string
		total, page, size, rows int64
	}{
		{"/api/audit?resource=key&page=3&limit=50", "events", 121, 3, 50, 21},
		{"/api/audit?resource=group&page=4&limit=10", "events", 0, 1, 10, 0},
		{"/api/members?page=5&limit=25", "members", 121, 5, 25, 21},
		{"/api/members?page=999&limit=100", "members", 121, 2, 100, 21},
		{"/api/keys?page=2&limit=100", "keys", 120, 2, 100, 20},
		{"/api/keys?include_revoked=true&page=5&limit=25", "keys", 121, 5, 25, 21},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := request(h, "GET", tc.path, "", nil, cookie)
			var result map[string]json.RawMessage
			if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil {
				t.Fatal(r.Code, r.Body.String())
			}
			for field, want := range map[string]int64{"total": tc.total, "page": tc.page, "page_size": tc.size} {
				var got int64
				if json.Unmarshal(result[field], &got) != nil || got != want {
					t.Errorf("%s=%d, want %d", field, got, want)
				}
			}
			var rows []json.RawMessage
			if json.Unmarshal(result[tc.field], &rows) != nil || int64(len(rows)) != tc.rows {
				t.Errorf("rows=%d, want %d", len(rows), tc.rows)
			}
		})
	}
	for _, path := range []string{"/api/audit", "/api/members", "/api/keys"} {
		for _, params := range []string{"page=-1", "page=bad", "limit=101", "limit=-1", "limit=bad", "cursor=1&page=2"} {
			if got := request(h, "GET", path+"?"+params, "", nil, cookie).Code; got != 400 {
				t.Errorf("%s?%s status=%d", path, params, got)
			}
		}
	}
}
