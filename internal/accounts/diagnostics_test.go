package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/logging"
)

func TestRefreshLogsOnlyBoundedFailureMetadata(t *testing.T) {
	s, account, _ := refreshFixture(t, "codex", time.Minute)
	var output bytes.Buffer
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&output, nil)))
	_, _ = s.Prepare(ctx, account.ID, func(context.Context, Credential) (Credential, error) {
		return Credential{}, &RefreshFailure{Status: 401, Code: "synthetic-private-code", Err: errors.New("synthetic-private-body")}
	})
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["error_code"] != "unknown" || record["upstream_status"] != float64(401) || record["account_id"] != account.ID {
		t.Fatal("missing safe credential diagnostics")
	}
	for _, secret := range []string{"synthetic-private", "synthetic-old", "synthetic-refresh"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("credential diagnostics leaked %s", secret)
		}
	}
}
