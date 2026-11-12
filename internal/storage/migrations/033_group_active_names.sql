-- Retired groups keep their original names: uniqueness applies only to active groups, so deleting
-- a group never collides with another group's label and released names can be reused.
-- Published migrations must remain an unchanged prefix for backup validation.
PRAGMA legacy_alter_table=ON;
CREATE TABLE account_groups_next (
 id INTEGER PRIMARY KEY,
 name TEXT NOT NULL COLLATE NOCASE,
 enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 restricted_models INTEGER NOT NULL DEFAULT 0 CHECK(restricted_models IN (0,1)),
 tenant_id INTEGER NOT NULL DEFAULT 1 REFERENCES tenants(id),
 routing_preference TEXT NOT NULL DEFAULT 'protocol' CHECK(routing_preference IN ('protocol','subscription_first','api_first')),
 allow_api_fallback INTEGER NOT NULL DEFAULT 1 CHECK(allow_api_fallback IN (0,1)),
 session_sticky INTEGER NOT NULL DEFAULT 0 CHECK(session_sticky IN (0,1)),
 deleted_at INTEGER
);
INSERT INTO account_groups_next(id,name,enabled,created_at,updated_at,restricted_models,tenant_id,routing_preference,allow_api_fallback,session_sticky,deleted_at)
SELECT id,name,enabled,created_at,updated_at,restricted_models,tenant_id,routing_preference,allow_api_fallback,session_sticky,deleted_at FROM account_groups;
DROP TABLE account_groups;
ALTER TABLE account_groups_next RENAME TO account_groups;
PRAGMA legacy_alter_table=OFF;
CREATE INDEX account_groups_tenant ON account_groups(tenant_id, id);
CREATE UNIQUE INDEX account_groups_active_name ON account_groups(tenant_id, name) WHERE deleted_at IS NULL;
CREATE TRIGGER account_groups_tenant_update BEFORE UPDATE OF tenant_id ON account_groups
WHEN NEW.tenant_id != OLD.tenant_id
BEGIN SELECT RAISE(ABORT, 'tenant_immutable'); END;
