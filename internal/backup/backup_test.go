package backup

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/alerts"
	"github.com/murongg/SubLane/internal/apikey"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/storage"
	"github.com/murongg/SubLane/internal/vault"
)

func TestBackupVerifiesWebhookOnlyCredentials(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	conn, err := storage.Open(ctx, filepath.Join(dir, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	identity, err := auth.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Setup(ctx, "synthetic-owner", "synthetic-password", "Synthetic"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, keyName)
	v, err := vault.Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := alerts.New(conn, v, nil).Update(ctx, 1, alerts.Input{URL: "https://hooks.example.test/events"}); err != nil {
		t.Fatal(err)
	}
	if err := verifyCredentials(ctx, conn, filepath.Join(dir, keyName)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyCredentials(ctx, conn, filepath.Join(dir, keyName)); err == nil {
		t.Fatal("incorrect webhook key accepted")
	}
}

func TestBackupRoundTripPreservesDataAndRevokesRestoredSessions(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	connection, err := storage.Open(ctx, filepath.Join(source, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	session, err := identity.Setup(ctx, "synthetic-admin", "synthetic-pass", "Synthetic workspace")
	if err != nil {
		t.Fatal(err)
	}
	member, err := identity.CreateMember(ctx, "synthetic-member", "synthetic-pass")
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := vault.Open(filepath.Join(source, keyName), true)
	if err != nil {
		t.Fatal(err)
	}
	idToken := "e30." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"synthetic-subject","chatgpt_user_id":"synthetic-member"}}`)) + ".synthetic"
	account, err := accounts.New(connection, cipher).Authorize(ctx, "Synthetic account", accounts.Credential{AccountID: "synthetic-subject", AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", IDToken: idToken, ExpiresAt: time.Now().Add(time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := groups.New(connection).Save(ctx, 0, groups.Input{Name: "Synthetic pool", Enabled: true, AccountIDs: []string{account.ID}})
	if err != nil {
		t.Fatal(err)
	}
	key, err := apikey.New(connection, cipher).CreateInGroup(ctx, 1, 1, "Synthetic key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec("PRAGMA wal_autocheckpoint=0; INSERT INTO settings(key,value) VALUES('synthetic.backup','before')"); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "test.sublane-backup.tar.gz")
	info, err := Create(ctx, source, output, "synthetic-version", Options{Password: syntheticBackupPassword})
	if err != nil {
		t.Fatal(err)
	}
	schemaVersion, err := storage.CurrentSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if info.SchemaVersion != schemaVersion || info.Version != "synthetic-version" {
		t.Fatal(info)
	}
	verified, err := Verify(ctx, output, syntheticBackupPassword)
	if err != nil || verified.DatabaseBytes <= 0 {
		t.Fatal(verified, err)
	}
	if _, err := connection.Exec("UPDATE settings SET value='after' WHERE key='synthetic.backup'"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	if _, err := Restore(ctx, output, target, syntheticBackupPassword); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.Open(ctx, filepath.Join(target, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restoredVault, err := vault.Open(filepath.Join(target, keyName), false)
	if err != nil {
		t.Fatal(err)
	}
	if secret, err := apikey.New(restored, restoredVault).Reveal(ctx, 1, key.Key.ID); err != nil || secret != key.Secret {
		t.Fatal("key not recoverable", err)
	}
	credential, err := accounts.New(restored, restoredVault).Prepare(ctx, account.ID, func(context.Context, accounts.Credential) (accounts.Credential, error) {
		t.Fatal("unexpected provider refresh")
		return accounts.Credential{}, nil
	})
	if err != nil || credential.AccessToken != "synthetic-access" || credential.AccountID != "synthetic-subject" || credential.IDToken != idToken {
		t.Fatal("credential restore", err)
	}
	if got, err := groups.New(restored).Get(ctx, pool.ID); err != nil || len(got.AccountIDs) != 1 {
		t.Fatal("group restore", err)
	}
	restoredIdentity, err := auth.New(restored)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := restoredIdentity.State(ctx, session.Token); err != nil || state.User != nil {
		t.Fatal("restored session still authorized", err)
	}
	if login, err := restoredIdentity.Login(ctx, "synthetic-member", "synthetic-pass"); err != nil || login.User.ID != member.ID {
		t.Fatal("member restore", err)
	}
	var value string
	if err := restored.QueryRow("SELECT value FROM settings WHERE key='synthetic.backup'").Scan(&value); err != nil || value != "before" {
		t.Fatal("snapshot settings changed", err)
	}
	if state, err := identity.State(ctx, session.Token); err != nil || state.User == nil {
		t.Fatal("backup changed source sessions", err)
	}
	if _, err := Create(ctx, source, output, "synthetic-version", Options{Password: syntheticBackupPassword}); err == nil {
		t.Fatal("overwrote backup")
	}
	if _, err := Restore(ctx, output, target, syntheticBackupPassword); err == nil {
		t.Fatal("overwrote restored data")
	}
	if _, err := Restore(ctx, output, source, syntheticBackupPassword); err == nil {
		t.Fatal("overwrote live source")
	}
	for _, path := range []string{output, filepath.Join(target, databaseName), filepath.Join(target, keyName)} {
		stat, err := os.Stat(path)
		if err != nil || stat.Mode().Perm()&0077 != 0 {
			t.Fatal("insecure restored file", err)
		}
	}
}

func TestFormerProxyCheckArchiveStillRestores(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	connection, err := storage.Open(ctx, filepath.Join(source, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := auth.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Setup(ctx, "synthetic-admin", "synthetic-pass", "Synthetic workspace"); err != nil {
		t.Fatal(err)
	}
	cipher, err := vault.Open(filepath.Join(source, keyName), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.New(connection, cipher).CreateProxy(ctx, "Synthetic exit", "http://127.0.0.1:18080"); err != nil {
		t.Fatal(err)
	}
	// Keep this archive on its historical schema so restore exercises newer migrations.
	if _, err := connection.ExecContext(ctx, `DROP TABLE usage_minutes;
DROP TABLE auth_challenges;
DROP TABLE passkeys;
DROP TABLE recovery_codes;
DROP INDEX users_passkey_handle;
ALTER TABLE users DROP COLUMN passkey_handle;
ALTER TABLE users DROP COLUMN security_version;
ALTER TABLE users DROP COLUMN totp_step;
ALTER TABLE users DROP COLUMN totp_secret;
DROP VIEW routable_group_resources;
DROP TABLE reset_runs;
DROP INDEX reset_checks;
ALTER TABLE accounts DROP COLUMN reset_config;
ALTER TABLE accounts DROP COLUMN reset_revision;
ALTER TABLE accounts DROP COLUMN reset_next_run_at;
ALTER TABLE accounts DROP COLUMN reset_time_zone;
ALTER TABLE accounts DROP COLUMN reset_check_at;
ALTER TABLE accounts DROP COLUMN usage_revision;
ALTER TABLE account_affinity DROP COLUMN rebound;
ALTER TABLE account_affinity DROP COLUMN model;
DROP INDEX account_groups_active_name;
ALTER TABLE allocation_entries DROP COLUMN image_input_tokens;
ALTER TABLE account_affinity DROP COLUMN derived;
ALTER TABLE account_groups DROP COLUMN session_sticky;
ALTER TABLE account_groups DROP COLUMN deleted_at;
ALTER TABLE account_groups DROP COLUMN routing_preference;
ALTER TABLE account_groups DROP COLUMN allow_api_fallback;
DROP TABLE account_model_runtime;
DROP TABLE alert_states;
ALTER TABLE tenants DROP COLUMN content_config;
ALTER TABLE request_records DROP COLUMN content_mode;
ALTER TABLE request_records DROP COLUMN content_revision;
ALTER TABLE request_records DROP COLUMN content_rule_ids;
ALTER TABLE request_records DROP COLUMN content_check_failed;
ALTER TABLE request_records DROP COLUMN route_explanation;
ALTER TABLE tenants DROP COLUMN alert_config;
ALTER TABLE memberships DROP COLUMN removed_at;
ALTER TABLE memberships DROP COLUMN first_request_at;
ALTER TABLE request_records DROP COLUMN reasoning_effort;
ALTER TABLE memberships DROP COLUMN max_concurrency;
ALTER TABLE memberships ADD COLUMN max_concurrency INTEGER NOT NULL DEFAULT 0 CHECK(max_concurrency BETWEEN 0 AND 8);
ALTER TABLE accounts DROP COLUMN max_concurrency;
ALTER TABLE accounts ADD COLUMN max_concurrency INTEGER NOT NULL DEFAULT 2 CHECK(max_concurrency BETWEEN 1 AND 8);
DELETE FROM settings WHERE key IN ('usage.keys.started_at','usage.minutes.started_at');
DELETE FROM schema_migrations WHERE name >= '006_reasoning_effort.sql';
INSERT INTO schema_migrations(name) VALUES('004_proxy_checks.sql')`); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(filepath.Join(source, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	info := Info{CreatedAt: time.Now().UTC().Truncate(time.Second), Version: "synthetic-version", SchemaVersion: 6, DatabaseBytes: stat.Size()}
	archive := filepath.Join(t.TempDir(), "former-proxy.sublane-backup.tar.gz")
	if err := writeArchive(ctx, source, archive, info, "", ""); err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(ctx, archive, syntheticBackupPassword)
	if err != nil || verified.SchemaVersion != 6 {
		t.Fatalf("former archive verification: %+v %v", verified, err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	if _, err := Restore(ctx, archive, target, syntheticBackupPassword); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.Open(ctx, filepath.Join(target, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var migrationCount int
	wantMigrations, err := storage.CurrentSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&migrationCount); err != nil || migrationCount != wantMigrations {
		t.Fatalf("restored migration history: %d %v", migrationCount, err)
	}
	restoredVault, err := vault.Open(filepath.Join(target, keyName), false)
	if err != nil {
		t.Fatal(err)
	}
	proxies, err := accounts.New(restored, restoredVault).ListProxies(ctx)
	if err != nil || len(proxies) != 1 || proxies[0].Endpoint != "http://127.0.0.1:18080" {
		t.Fatalf("restored proxy: %+v %v", proxies, err)
	}
}
