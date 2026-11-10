package apikey

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/storage"
	"github.com/murongg/SubLane/internal/vault"
)

func newTestKeys(t *testing.T, connection *sql.DB) *Service {
	t.Helper()
	configureTestPool(t, connection)
	cipher, err := vault.Open(filepath.Join(t.TempDir(), "credentials.key"), true)
	if err != nil {
		t.Fatal(err)
	}
	return New(connection, cipher)
}
func TestEncryptedKeyDisclosureOwnershipAndLifecycle(t *testing.T) {
	base := context.Background()
	directory := t.TempDir()
	path := filepath.Join(directory, "keys.db")
	connection, err := storage.Open(base, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { connection.Close() }()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Setup(base, "synthetic-admin", "synthetic-pass", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	member, err := identity.CreateMember(base, "synthetic-member", "synthetic-pass")
	if err != nil {
		t.Fatal(err)
	}
	ctx := audit.WithActor(base, audit.Actor{TenantID: 1, ID: member.ID, Username: member.Username, Role: "member", Source: "user"})
	keyPath := filepath.Join(directory, "credentials.key")
	cipher, err := vault.Open(keyPath, true)
	if err != nil {
		t.Fatal(err)
	}
	configureTestPool(t, connection)
	keys := New(connection, cipher)
	created, err := keys.CreateInGroup(ctx, member.ID, 1, "Synthetic recoverable key")
	if err != nil || !created.Key.Copyable {
		t.Fatal("new key not copyable", err)
	}
	var encrypted []byte
	if err := connection.QueryRow("SELECT encrypted_secret FROM api_keys WHERE id=?", created.Key.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(created.Secret)) {
		t.Fatal("plaintext persisted")
	}
	for range 2 {
		value, err := keys.Reveal(ctx, member.ID, created.Key.ID)
		if err != nil || value != created.Secret {
			t.Fatal("cannot repeat disclosure", err)
		}
	}
	if value, err := keys.Reveal(ctx, 1, created.Key.ID); !errors.Is(err, ErrNotFound) || value != "" {
		t.Fatal("admin read another user's key")
	}
	page, err := keys.List(ctx, member.ID, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), created.Secret) || strings.Contains(string(raw), "token_hash") || strings.Contains(string(raw), "encrypted") {
		t.Fatal("secret leaked into list")
	}
	if _, err := keys.Update(ctx, member.ID, created.Key.ID, UpdateInput{Name: "Paused", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if value, err := keys.Reveal(ctx, member.ID, created.Key.ID); err != nil || value != created.Secret {
		t.Fatal("paused key lost secret", err)
	}
	if _, err := connection.Exec("CREATE TRIGGER fail_audit BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT,'synthetic failure'); END"); err != nil {
		t.Fatal(err)
	}
	if value, err := keys.Reveal(ctx, member.ID, created.Key.ID); err == nil || value != "" {
		t.Fatal("disclosed without audit commit")
	}
	if _, err := keys.Revoke(ctx, member.ID, created.Key.ID); err == nil {
		t.Fatal("revoked without audit")
	}
	var retained int
	if err := connection.QueryRow("SELECT count(*) FROM api_keys WHERE encrypted_secret IS NOT NULL").Scan(&retained); err != nil || retained != 1 {
		t.Fatal("failed revocation erased secret", err)
	}
	if _, err := connection.Exec("DROP TRIGGER fail_audit"); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	connection, err = storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err = vault.Open(keyPath, false)
	if err != nil {
		t.Fatal(err)
	}
	keys = New(connection, cipher)
	if err := keys.Verify(ctx); err != nil {
		t.Fatal("startup verification", err)
	}
	if value, err := keys.Reveal(ctx, member.ID, created.Key.ID); err != nil || value != created.Secret {
		t.Fatal("reopen lost secret", err)
	}
	events, err := audit.New(connection).List(ctx, audit.Filter{Resource: "key"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(events)
	if strings.Contains(string(raw), created.Secret) {
		t.Fatal("audit exposed key")
	}
	if events.Events[0].Action != "key.reveal" {
		t.Fatal("missing disclosure event")
	}
	revoked, err := keys.Revoke(ctx, member.ID, created.Key.ID)
	if err != nil || revoked.Copyable {
		t.Fatal("revoked key remains copyable", err)
	}
	if value, err := keys.Reveal(ctx, member.ID, created.Key.ID); !errors.Is(err, ErrRevoked) || value != "" {
		t.Fatal("revoked key disclosed")
	}
	var count int
	if err := connection.QueryRow("SELECT count(*) FROM api_keys WHERE encrypted_secret IS NOT NULL").Scan(&count); err != nil || count != 0 {
		t.Fatal("revocation retained encrypted key", err)
	}
}
func TestLegacyKeysAndCorruptSecretsFailDisclosure(t *testing.T) {
	ctx := context.Background()
	connection, err := storage.Open(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Setup(ctx, "synthetic-admin", "synthetic-pass", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	keys := newTestKeys(t, connection)
	secret := "sl_" + strings.Repeat("x", 43)
	digest := sha256.Sum256([]byte(secret))
	if _, err := connection.Exec("INSERT INTO api_keys(id,user_id,group_id,name,prefix,token_hash,created_at) VALUES(1,1,1,'Synthetic legacy key','sl_xxxxxxxx',?,123)", digest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Authenticate(ctx, secret); err != nil {
		t.Fatal("legacy authentication changed", err)
	}
	page, err := keys.List(ctx, 1, Filter{})
	if err != nil || page.Keys[0].Copyable {
		t.Fatal("legacy key falsely copyable", err)
	}
	if value, err := keys.Reveal(ctx, 1, 1); !errors.Is(err, ErrNotCopyable) || value != "" {
		t.Fatal("legacy secret invented", err)
	}
	created, err := keys.CreateInGroup(ctx, 1, 1, "Encrypted")
	if err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	if err := connection.QueryRow("SELECT encrypted_secret FROM api_keys WHERE id=?", created.Key.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if err := newTestKeys(t, connection).Verify(ctx); err == nil {
		t.Fatal("wrong master key accepted")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err := connection.Exec("UPDATE api_keys SET encrypted_secret=? WHERE id=?", encrypted, created.Key.ID); err != nil {
		t.Fatal(err)
	}
	if err := keys.Verify(ctx); err == nil {
		t.Fatal("corrupt secret passed startup")
	}
	if value, err := keys.Reveal(ctx, 1, created.Key.ID); err == nil || value != "" {
		t.Fatal("corrupt secret disclosed")
	}
	if _, err := keys.Authenticate(ctx, created.Secret); err != nil {
		t.Fatal("gateway auth stopped using hashes", err)
	}
}

func configureTestPool(t *testing.T, conn *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"INSERT OR IGNORE INTO account_groups(id,name,enabled,created_at,updated_at) VALUES(1,'Default',1,1,1)",
		"INSERT OR IGNORE INTO group_accounts(group_id,account_id) SELECT 1,id FROM accounts",
		"INSERT OR IGNORE INTO group_members SELECT 1,id FROM users",
	} {
		if _, err := conn.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}
