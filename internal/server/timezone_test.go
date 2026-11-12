package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/testdb"
	"github.com/murongg/SubLane/internal/timezone"
)

func TestTimeZoneSettingIsGlobalAndPlatformOwnerOnly(t *testing.T) {
	ctx := context.Background()
	conn, err := testdb.Open(ctx, filepath.Join(t.TempDir(), "synthetic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	identity, err := auth.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	zones, err := timezone.New(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Auth: identity, Audit: audit.New(conn), TimeZone: zones})
	origin := "http://example.test"
	setup := request(h, "POST", "/api/auth/setup", origin, map[string]string{"username": "synthetic-admin", "password": "synthetic-pass", "workspace_name": "Synthetic workspace"}, nil)
	admin := setup.Result().Cookies()[0]
	memberInput := map[string]string{"username": "synthetic-member", "password": "synthetic-pass"}
	if result := request(h, "POST", "/api/members", origin, memberInput, admin); result.Code != 201 {
		t.Fatal(result.Code)
	}
	member := request(h, "POST", "/api/auth/login", origin, memberInput, nil).Result().Cookies()[0]
	path := "/api/settings/timezone"
	for _, method := range []string{"GET", "PATCH"} {
		if result := request(h, method, path, origin, map[string]string{"time_zone": "Asia/Shanghai"}, member); result.Code != 403 {
			t.Fatal(method, result.Code)
		}
	}
	if result := request(h, "PATCH", path, origin, map[string]string{"time_zone": "Local"}, admin); result.Code != 400 {
		t.Fatal(result.Code)
	}
	if result := request(h, "PATCH", path, origin, map[string]string{"time_zone": "Asia/Shanghai"}, admin); result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
	var state struct {
		TimeZone string `json:"time_zone"`
	}
	if err := json.Unmarshal(request(h, "GET", "/api/auth/state", "", nil, member).Body.Bytes(), &state); err != nil || state.TimeZone != "Asia/Shanghai" {
		t.Fatal(state, err)
	}
	if zones.Name() != "Asia/Shanghai" {
		t.Fatal(zones.Name())
	}
}
