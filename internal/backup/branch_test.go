package backup

import (
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/storage"
	"github.com/murongg/SubLane/internal/vault"
)

func TestBranchArchivesAndDatabasesUpgradeWithoutLosingData(t *testing.T) {
	branches := map[string][]string{
		"main":   {"030_group_lifecycle.sql", "031_image_requests.sql", "032_affinity_preferences.sql", "033_group_active_names.sql", "034_allocation_image_input.sql"},
		"deploy": {"030_group_preference_boosts.sql", "031_scheduling_policy.sql", "032_scheduling_history.sql", "033_scheduling_cycles.sql"},
	}
	for branch, tail := range branches {
		t.Run(branch, func(t *testing.T) {
			ctx := t.Context()
			source := t.TempDir()
			path := filepath.Join(source, databaseName)
			conn, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetMaxOpenConns(1)
			if _, err := conn.Exec(`CREATE TABLE schema_migrations(name TEXT PRIMARY KEY NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			// Build the actual published schema, without manufacturing a subset of migration records.
			directory := filepath.Join("..", "storage", "migrations")
			files, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			version := 0
			for _, file := range files {
				if file.Name() >= "030_" && !slices.Contains(tail, file.Name()) {
					continue
				}
				data, err := os.ReadFile(filepath.Join(directory, file.Name()))
				if err != nil {
					t.Fatal(err)
				}
				// Historical rebuilds require the same FK isolation as the production runner.
				if _, err := conn.Exec("PRAGMA foreign_keys=OFF"); err != nil {
					t.Fatal(err)
				}
				if _, err := conn.Exec(string(data)); err != nil {
					t.Fatal(file.Name(), err)
				}
				if _, err := conn.Exec(`INSERT INTO schema_migrations(name) VALUES(?)`, file.Name()); err != nil {
					t.Fatal(err)
				}
				version++
			}
			if _, err := conn.Exec("PRAGMA foreign_keys=ON; PRAGMA legacy_alter_table=OFF"); err != nil {
				t.Fatal(err)
			}
			identity, err := auth.New(conn)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := identity.Setup(ctx, "synthetic-admin", "synthetic-pass", "Synthetic workspace"); err != nil {
				t.Fatal(err)
			}
			key, err := vault.Open(filepath.Join(source, keyName), true)
			if err != nil {
				t.Fatal(err)
			}
			account, err := accounts.New(conn, key).Authorize(ctx, "Synthetic account", accounts.Credential{AccountID: "synthetic-subject", AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix()}, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(`INSERT INTO account_groups(id,name,enabled,created_at,updated_at) VALUES(1,'Synthetic group',1,1,1)`); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(`INSERT INTO group_accounts(group_id,account_id) VALUES(1,?)`, account.ID); err != nil {
				t.Fatal(err)
			}
			for _, statement := range []string{
				`INSERT INTO group_members(group_id,user_id) VALUES(1,1)`,
				`INSERT INTO api_keys(id,user_id,group_id,name,prefix,token_hash,created_at) VALUES(1,1,1,'Synthetic key','sl_test',zeroblob(32),1)`,
				`INSERT INTO allocation_schemes(id,name,group_id,enabled,created_at) VALUES(1,'Synthetic allowance',1,1,1)`,
				`INSERT INTO allocation_revisions(id,scheme_id,effective_at,accounting_anchor_at,config) VALUES(1,1,1,1,'{}')`,
				`INSERT INTO allocation_keys(key_id,scheme_id) VALUES(1,1)`,
				`INSERT INTO allocation_entries(request_id,scheme_id,revision_id,user_id,account_id,model,mode,window_start,reset_at,started_at,state,input_tokens,cost) VALUES('synthetic-settled',1,1,1,'synthetic-account','synthetic-model','tokens',1,2147483647,1,'settled',7,7)`,
				`INSERT INTO request_records(user_id,key_id,group_id,model,transport,operation,started_at,duration_ms,outcome,input_tokens) VALUES(1,1,1,'synthetic-model','http','responses',1,1,'success',7)`,
			} {
				if _, err := conn.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := conn.Exec(`INSERT INTO account_affinity(user_id,group_id,session_hash,provider,account_id,expires_at,model) VALUES(1,1,zeroblob(32),'auto',?,2147483647,'synthetic-model')`, account.ID); err != nil {
				t.Fatal(err)
			}
			if branch == "deploy" {
				if _, err := conn.Exec(`UPDATE group_accounts SET preference_boost=125; UPDATE tenants SET scheduling_revision=3; INSERT INTO scheduling_cycles(tenant_id,account_id,scope,lifecycle,reset_at,window_seconds,remaining,observation_age,observations,complete,policy_revision) VALUES(1,'synthetic-account','synthetic-window',0,2147483647,18000,25,1,5,1,3)`); err != nil {
					t.Fatal(err)
				}
			} else if _, err := conn.Exec(`UPDATE account_groups SET session_sticky=1; UPDATE allocation_entries SET image_input_tokens=2`); err != nil {
				t.Fatal(err)
			}
			if got, err := storage.ValidateSnapshot(ctx, conn); err != nil || got != version {
				t.Fatal("published snapshot rejected", got, version, err)
			}
			conn.Close()
			stat, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(t.TempDir(), "synthetic-branch.sublane-backup.tar.gz")
			info := Info{CreatedAt: time.Now().UTC().Truncate(time.Second), Version: "synthetic-" + branch, SchemaVersion: version, DatabaseBytes: stat.Size()}
			if err := writeArchive(ctx, source, archive, info, "", syntheticBackupPassword); err != nil {
				t.Fatal(err)
			}
			if got, err := Verify(ctx, archive, syntheticBackupPassword); err != nil || got.SchemaVersion != version {
				t.Fatal("branch archive verification", got, err)
			}
			target := filepath.Join(t.TempDir(), "restored")
			if _, err := Restore(ctx, archive, target, syntheticBackupPassword); err != nil {
				t.Fatal("branch archive restore", err)
			}
			original, err := storage.OpenReadOnly(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := storage.ValidateSnapshot(ctx, original); err != nil || got != version {
				t.Fatal("archive operations changed the source", got, err)
			}
			original.Close()
			for _, database := range []string{filepath.Join(target, databaseName), path} {
				upgraded, err := storage.Open(ctx, database)
				if err != nil {
					t.Fatal("database upgrade", err)
				}
				var entries, usage, records, keys, bindings, boost, sticky, image, cycles, revision int
				err = upgraded.QueryRow(`SELECT (SELECT count(*) FROM allocation_entries),(SELECT sum(cost) FROM allocation_entries),(SELECT count(*) FROM request_records),(SELECT count(*) FROM allocation_keys),(SELECT count(*) FROM account_affinity),(SELECT preference_boost FROM group_accounts LIMIT 1),(SELECT session_sticky FROM account_groups LIMIT 1),(SELECT image_input_tokens FROM allocation_entries LIMIT 1),(SELECT count(*) FROM scheduling_cycles),(SELECT scheduling_revision FROM tenants LIMIT 1)`).Scan(&entries, &usage, &records, &keys, &bindings, &boost, &sticky, &image, &cycles, &revision)
				if err != nil || entries != 1 || usage != 7 || records != 1 || keys != 1 || bindings != 1 {
					t.Fatal("upgrade lost history or bindings", entries, usage, records, keys, bindings, err)
				}
				if branch == "deploy" && (boost != 125 || cycles != 1 || revision != 3) || branch == "main" && (sticky != 1 || image != 2) {
					t.Fatal("upgrade lost branch settings", boost, sticky, image, cycles, revision)
				}
				if _, err := storage.ValidateSnapshot(ctx, upgraded); err != nil {
					t.Fatal(err)
				}
				upgraded.Close()
			}
		})
	}
}
