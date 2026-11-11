package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/auth"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/upstream"
)

func TestHiddenManagementCatalogLimitDoesNotBlockPublicDiscovery(t *testing.T) {
	ctx := context.Background()
	s, _ := discoveryGateway(t, transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unexpected synthetic IO") }))
	ids := []string{}
	for index := range 9 {
		account, err := s.accounts.Authorize(ctx, "Synthetic large catalog", accounts.Credential{AccountID: fmt.Sprintf("synthetic-large-%d", index), AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, account.ID)
		hidden := []string{}
		for model := range 511 {
			hidden = append(hidden, fmt.Sprintf("synthetic-hidden-%d-%d", index, model))
		}
		models := append(slices.Clone(hidden), "synthetic-visible")
		if err := s.accounts.SaveCatalog(ctx, account.ID, 0, models, s.now().Unix(), s.provider.CatalogSource("codex"), hidden...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := groups.New(s.db).Save(ctx, 1, groups.Input{Name: "Synthetic large catalog", Enabled: true, AccountIDs: ids}); err != nil {
		t.Fatal(err)
	}
	models, err := s.Models(ctx, 1, 1)
	if err != nil || len(models) != 1 || models[0].ID != "synthetic-visible" {
		t.Fatal("hidden management metadata blocked public discovery", len(models), err)
	}
	if _, err := s.GroupCatalog(ctx, 1, 1, false); !errors.Is(err, upstream.ErrResponse) {
		t.Fatal("management catalog lost its existing total model bound", err)
	}
}

func TestManagementCatalogMarksHiddenModelsWithoutChangingPublicDiscovery(t *testing.T) {
	for _, bothHidden := range []bool{false, true} {
		t.Run(map[bool]string{false: "visible route wins", true: "all routes hidden"}[bothHidden], func(t *testing.T) {
			ctx := context.Background()
			s, ids := discoveryGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
				t.Error("fresh management catalog contacted upstream")
				return nil, errors.New("unexpected synthetic IO")
			}))
			for _, provider := range []string{"codex", "claude"} {
				hidden := []string{"synthetic-hidden"}
				if provider == "codex" || bothHidden {
					hidden = append(hidden, "synthetic-shared")
				}
				if err := s.accounts.SaveCatalog(ctx, ids[provider], 0, []string{"synthetic-hidden", "synthetic-shared", "synthetic-visible"}, s.now().Unix(), s.provider.CatalogSource(provider), hidden...); err != nil {
					t.Fatal(err)
				}
			}
			input := groups.Input{Name: "Synthetic catalog", Enabled: true, AccountIDs: []string{ids["codex"], ids["claude"]}}
			if _, err := groups.New(s.db).Save(ctx, 1, input); err != nil {
				t.Fatal(err)
			}
			for _, allowed := range [][]string{nil, {"codex/synthetic-hidden(high)"}, {"claude/synthetic-shared"}, {"synthetic-visible"}} {
				input.ModelPolicy = &groups.ModelPolicy{Restricted: allowed != nil, Models: allowed}
				if _, err := groups.New(s.db).Save(ctx, 1, input); err != nil {
					t.Fatal(err)
				}
				catalog, err := s.GroupCatalog(ctx, 1, 1, false)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(catalog)
				var view struct {
					HiddenModels []string `json:"hidden_models"`
				}
				if err != nil || json.Unmarshal(raw, &view) != nil {
					t.Fatal(err)
				}
				want := []string{}
				switch {
				case allowed == nil:
					want = append(want, "synthetic-hidden")
					if bothHidden {
						want = append(want, "synthetic-shared")
					}
				case allowed[0] == "codex/synthetic-hidden(high)":
					want = append(want, "synthetic-hidden(high)")
				case allowed[0] == "claude/synthetic-shared" && bothHidden:
					want = append(want, "synthetic-shared")
				}
				if !slices.Equal(view.HiddenModels, want) {
					t.Fatalf("policy-scoped hidden models: got %s, want %v", raw, want)
				}
				public, err := s.Models(ctx, 1, 1)
				if err != nil {
					t.Fatal(err)
				}
				for _, model := range public {
					if model.ID == "synthetic-shared" && model.OwnedBy != "claude" {
						t.Fatal("a hidden route changed the public model owner", model)
					}
				}
				encoded, _ := json.Marshal(public)
				if strings.Contains(string(encoded), "synthetic-hidden") || bothHidden && strings.Contains(string(encoded), "synthetic-shared") {
					t.Fatal("hidden metadata entered public discovery", string(encoded))
				}
			}
		})
	}
}

func TestCatalogUnionsAccountsAndRoutesOnlySupportedModels(t *testing.T) {
	ctx := context.Background()
	var selected string
	s, ids := discoveryGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			models := `{"models":[{"slug":"synthetic-shared"},{"slug":"synthetic-basic"}]}`
			if r.Header.Get("Chatgpt-Account-Id") == "synthetic-premium" {
				models = `{"models":[{"slug":"synthetic-shared"},{"slug":"synthetic-premium-only"}]}`
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(models))}, nil
		}
		selected = r.Header.Get("Chatgpt-Account-Id")
		return syntheticStream(), nil
	}))
	extra, err := s.accounts.Authorize(ctx, "Synthetic premium", accounts.Credential{AccountID: "synthetic-premium", AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}, "")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := groups.New(s.db).Save(ctx, 0, groups.Input{Name: "Mixed", Enabled: true, AccountIDs: []string{ids["codex"], extra.ID}})
	if err != nil {
		t.Fatal(err)
	}
	models, err := s.Models(ctx, 1, pool.ID)
	if err != nil || len(models) != 3 {
		t.Fatalf("expected three native models, got %+v: %v", models, err)
	}
	for range 3 {
		response, err := s.Open(ctx, 1, pool.ID, []byte(`{"model":"codex/synthetic-premium-only","input":"synthetic"}`), nil, Responses)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if selected != "synthetic-premium" {
			t.Fatal("model sent to an unsupported account")
		}
	}
	sticky := http.Header{"Session_id": {"synthetic-catalog-session"}}
	response, err := s.Open(ctx, 1, pool.ID, []byte(`{"model":"synthetic-premium-only","input":"synthetic"}`), sticky, Responses)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if _, err := s.Open(ctx, 1, pool.ID, []byte(`{"model":"synthetic-basic","input":"synthetic"}`), sticky, Responses); !errors.Is(err, ErrModelConflict) {
		t.Fatal("sticky model change moved the conversation", err)
	}
	if _, err := groups.New(s.db).Save(ctx, pool.ID, groups.Input{Name: pool.Name, Enabled: true, AccountIDs: []string{ids["codex"], extra.ID}, ModelPolicy: &groups.ModelPolicy{Restricted: true, Models: []string{"synthetic-shared"}}}); err != nil {
		t.Fatal(err)
	}
	models, err = s.Models(ctx, 1, pool.ID)
	if err != nil || len(models) != 1 || models[0].ID != "synthetic-shared" {
		t.Fatal("automatic catalog overwrote group policy", models, err)
	}
	if _, err := groups.New(s.db).Save(ctx, pool.ID, groups.Input{Name: pool.Name, Enabled: true, AccountIDs: []string{ids["codex"], extra.ID}, ModelPolicy: &groups.ModelPolicy{Restricted: true, Models: []string{"synthetic-shared(high)"}}}); err != nil {
		t.Fatal(err)
	}
	models, err = s.Models(ctx, 1, pool.ID)
	if err != nil || len(models) != 1 || models[0].ID != "synthetic-shared(high)" {
		t.Fatal("explicit thinking variant disappeared", models, err)
	}
	response, err = s.Open(ctx, 1, pool.ID, []byte(`{"model":"synthetic-shared(high)","input":"synthetic"}`), nil, Responses)
	if err != nil {
		t.Fatal("thinking variant could not route", err)
	}
	response.Body.Close()
}

func TestCatalogCacheSurvivesRestartAndRetainsFailedRefresh(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	var failing atomic.Bool
	s, ids := discoveryGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if failing.Load() {
			return nil, errors.New("synthetic failure")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"synthetic-model"}]}`))}, nil
	}))
	first, err := s.AccountCatalog(ctx, ids["codex"], true)
	if err != nil || !first.Known || len(first.Models) != 1 {
		t.Fatal(first, err)
	}
	reopened := New(ctx, s.db, s.accounts, s.provider)
	defer reopened.Close()
	cached, err := reopened.AccountCatalog(ctx, ids["codex"], false)
	if err != nil || calls.Load() != 1 || cached.UpdatedAt != first.UpdatedAt {
		t.Fatal("cache was not persistent", cached, err)
	}
	reopened.catalog.now = func() time.Time { return time.Unix(first.UpdatedAt, 0).Add(20 * time.Minute) }
	failing.Store(true)
	stale, err := reopened.AccountCatalog(ctx, ids["codex"], true)
	if err != nil || !stale.Stale || !stale.RefreshFailed || !stale.Known || stale.UpdatedAt != first.UpdatedAt || len(stale.Models) != 1 {
		t.Fatal("failed refresh lost snapshot", stale, err)
	}
	before := calls.Load()
	_, _ = reopened.AccountCatalog(ctx, ids["codex"], true)
	if calls.Load() != before {
		t.Fatal("failure backoff bypassed")
	}
	if _, err := s.accounts.SetEnabled(ctx, ids["codex"], false); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.AccountCatalog(ctx, ids["codex"], false); !errors.Is(err, accounts.ErrDisabled) {
		t.Fatal("disabled account served catalog", err)
	}
}

func TestCatalogEmptyObservationIsNotUnknown(t *testing.T) {
	ctx := context.Background()
	s, ids := discoveryGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":[]}`))}, nil
	}))
	if _, err := groups.New(s.db).Save(ctx, 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{ids["codex"]}}); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.AccountCatalog(ctx, ids["codex"], true)
	if err != nil || !catalog.Known || len(catalog.Models) != 0 {
		t.Fatal("known empty observation lost", catalog, err)
	}
	models, err := s.Models(ctx, 1, 1)
	if err != nil || len(models) != 0 {
		t.Fatal(models, err)
	}
	if _, err := s.Open(ctx, 1, 1, []byte(`{"model":"synthetic-unknown","input":"synthetic"}`), nil, Responses); !errors.Is(err, ErrModelUnavailable) {
		t.Fatal("unknown model was forwarded", err)
	}
	encoded, _ := json.Marshal(catalog)
	if strings.Contains(string(encoded), "synthetic-access") {
		t.Fatal("catalog leaked credentials")
	}
}

func TestHiddenModelRefreshesLegacyCatalogAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	var discoveries, forwarded int
	s, ids := discoveryGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			discoveries++
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"synthetic-model"},{"slug":"codex-auto-review","visibility":"hide"}]}`))}, nil
		}
		forwarded++
		return syntheticStream(), nil
	}))
	if _, err := groups.New(s.db).Save(ctx, 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{ids["codex"]}}); err != nil {
		t.Fatal(err)
	}
	if err := s.accounts.SaveCatalog(ctx, ids["codex"], 0, []string{"synthetic-model"}, time.Now().Unix(), "codex:v1:"+upstream.DefaultCodexVersion); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"model":"codex-auto-review","input":"Synthetic operation"}`)
	for _, service := range []*Service{s, New(ctx, s.db, s.accounts, s.provider)} {
		if service != s {
			defer service.Close()
		}
		stream, err := service.Open(ctx, 1, 1, raw, nil, Responses)
		if err != nil {
			t.Fatal("hidden capability did not route after legacy discovery or restart", err)
		}
		stream.Body.Close()
	}
	if discoveries != 1 || forwarded != 2 {
		t.Fatalf("capability was not persisted: discoveries=%d forwarded=%d", discoveries, forwarded)
	}
	if _, err := s.Open(ctx, 1, 1, []byte(`{"model":"synthetic-unknown","input":"Synthetic operation"}`), nil, Responses); !errors.Is(err, ErrModelUnavailable) {
		t.Fatal("unknown capability was forwarded", err)
	}
	availability, err := s.Availability(ctx, 1, 1, "codex-auto-review")
	if err != nil || availability.State != "available" {
		t.Fatal("hidden capability is unavailable to diagnostics", availability, err)
	}
	pools, err := s.WorkspaceCapacity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, pool := range pools {
		for _, model := range pool.Models {
			if model.Model == "codex-auto-review" {
				t.Fatal("hidden capability entered the monitored model list")
			}
		}
	}
}

func TestCatalogRefreshCoalescesAndRejectsReauthorizationRace(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	s, ids := discoveryGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"synthetic-old"}]}`))}, nil
	}))
	first, err := s.AccountCatalog(ctx, ids["codex"], false)
	if err != nil || first.Known || !first.Refreshing {
		t.Fatal(first, err)
	}
	<-started
	for range 5 {
		value, err := s.AccountCatalog(ctx, ids["codex"], false)
		if err != nil || !value.Refreshing {
			t.Fatal(value, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent reads duplicated discovery")
	}
	_, err = s.accounts.Authorize(ctx, "Synthetic reauthorized", accounts.Credential{AccountID: "synthetic-subject", AccessToken: "synthetic-new", RefreshToken: "synthetic-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}, ids["codex"])
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	s.catalog.mu.Lock()
	flight := s.catalog.entries[ids["codex"]].flight
	s.catalog.mu.Unlock()
	if flight != nil {
		<-flight
	}
	saved, err := s.accounts.Catalog(ctx, ids["codex"])
	if err != nil || saved.UpdatedAt != 0 {
		t.Fatal("old authorization published a snapshot", saved, err)
	}
}

func TestCatalogShutdownCancelsRefreshAndExpiredSnapshotsCannotRoute(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{})
	s, ids := discoveryGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	catalog, _ := s.accounts.Catalog(ctx, ids["codex"])
	if err := s.accounts.SaveCatalog(ctx, ids["codex"], catalog.Revision, []string{"synthetic-model"}, time.Now().Add(-25*time.Hour).Unix(), upstream.CatalogSource("codex")); err != nil {
		t.Fatal(err)
	}
	if _, err := groups.New(s.db).Save(ctx, 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{ids["codex"]}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Models(ctx, 1, 1); !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatal("expired catalog advertised", err)
	}
	<-started
	if _, err := s.Open(ctx, 1, 1, []byte(`{"model":"synthetic-model","input":"synthetic"}`), nil, Responses); !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatal("expired catalog routed", err)
	}
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel catalog refresh")
	}
}

func TestCatalogCannotPublishAcrossGroupAccessRevocation(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	s, ids := discoveryGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"synthetic-private-model"}]}`))}, nil
	}))
	identity, err := auth.New(s.db)
	if err != nil {
		t.Fatal(err)
	}
	member, err := identity.CreateMember(ctx, "synthetic-catalog-member", "synthetic-pass")
	if err != nil {
		t.Fatal(err)
	}
	manager := groups.New(s.db)
	pool, err := manager.Save(ctx, 0, groups.Input{Name: "Synthetic private pool", Enabled: true, AccountIDs: []string{ids["codex"]}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetMemberGroups(ctx, member.ID, []int64{pool.ID}); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := s.Models(ctx, member.ID, pool.ID); finished <- err }()
	<-started
	if err := manager.SetMemberGroups(ctx, member.ID, []int64{}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-finished; !errors.Is(err, groups.ErrUnavailable) {
		t.Fatal("catalog crossed revoked group access", err)
	}
}

func TestCatalogRefreshesAfterDiscoverySourceChanges(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	s, ids := discoveryGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"synthetic-current"},{"slug":"synthetic-new-release"}]}`))}, nil
	}))
	if _, err := groups.New(s.db).Save(ctx, 1, groups.Input{Name: "Default", Enabled: true, AccountIDs: []string{ids["codex"]}}); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"", "codex:0.1.0"} {
		saved := map[string]any{"models": []string{"synthetic-current"}, "updated_at": time.Now().Unix(), "source": source}
		raw, _ := json.Marshal(saved)
		if _, err := s.db.Exec("UPDATE accounts SET models_snapshot=?,models_revision=models_revision+1 WHERE id=?", raw, ids["codex"]); err != nil {
			t.Fatal(err)
		}
		before := calls.Load()
		models, err := s.Models(ctx, 1, 1)
		if err != nil || len(models) != 2 || calls.Load() != before+1 {
			t.Fatalf("fresh timestamp masked obsolete discovery source: count=%d calls=%d err=%v", len(models), calls.Load(), err)
		}
		catalog, err := s.accounts.Catalog(ctx, ids["codex"])
		if err != nil || len(catalog.Models) != 2 || catalog.Source != upstream.CatalogSource("codex") {
			t.Fatal("new catalog not persisted", catalog, err)
		}
		if _, err := s.Models(ctx, 1, 1); err != nil || calls.Load() != before+1 {
			t.Fatal("current source was fetched again", err)
		}
	}
}

func TestVersionChangeDiscardsAnInFlightCatalog(t *testing.T) {
	ctx := context.Background()
	var version atomic.Value
	version.Store("0.200.0")
	started := make(chan struct{})
	release := make(chan struct{})
	s, ids := providerFixture(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("client_version") == "0.200.0" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"synthetic-model"}]}`))}, nil
	}), false, func() string { return version.Load().(string) })
	if _, err := s.AccountCatalog(ctx, ids["codex"], false); err != nil {
		t.Fatal(err)
	}
	<-started
	s.catalog.mu.Lock()
	flight := s.catalog.entries[ids["codex"]].flight
	s.catalog.mu.Unlock()
	version.Store("0.300.0")
	close(release)
	<-flight
	old, err := s.accounts.Catalog(ctx, ids["codex"])
	if err != nil || old.UpdatedAt != 0 {
		t.Fatal("outdated request published after a version change", old, err)
	}
	if _, err := s.Check(ctx, ids["codex"]); err != nil {
		t.Fatal("old backoff blocked new version discovery", err)
	}
	current, err := s.accounts.Catalog(ctx, ids["codex"])
	if err != nil || current.Source != "codex:v4:0.300.0" {
		t.Fatal(current, err)
	}
}

func TestCatalogPersistsAndConservativelyMergesCapabilities(t *testing.T) {
	ctx := context.Background()
	s, ids := discoveryGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"models":[{"slug":"synthetic-shared","context_window":200000,"max_output_tokens":16000,"supported_reasoning_levels":[{"effort":"low"},{"effort":"high"}],"supports_images":true,"supports_tools":true},{"slug":"synthetic-hidden","visibility":"hide","context_window":10000}]}`
		if r.URL.Host == "api.anthropic.com" {
			body = `{"data":[{"id":"synthetic-shared","context_window":100000,"max_output_tokens":8000,"supported_reasoning_levels":["high"],"supports_images":true,"supports_tools":false}]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	for _, provider := range []string{"codex", "claude"} {
		snapshot, err := s.AccountCatalog(ctx, ids[provider], true)
		if err != nil || !snapshot.Known {
			t.Fatal(snapshot, err)
		}
		raw, _ := json.Marshal(snapshot)
		if !strings.Contains(string(raw), `"capabilities"`) {
			t.Fatalf("account snapshot discarded metadata: %s", raw)
		}
	}
	if _, err := groups.New(s.db).Save(ctx, 1, groups.Input{Name: "Synthetic capabilities", Enabled: true, AccountIDs: []string{ids["codex"], ids["claude"]}}); err != nil {
		t.Fatal(err)
	}
	models, err := s.Models(ctx, 1, 1)
	if err != nil || len(models) != 1 {
		t.Fatal(models, err)
	}
	raw, _ := json.Marshal(models[0])
	var got map[string]any
	json.Unmarshal(raw, &got)
	if got["context_window"] != float64(100000) || got["max_output_tokens"] != float64(8000) || got["supports_images"] != true || got["supports_tools"] != false || !slices.Equal(got["supported_reasoning_levels"].([]any), []any{"high"}) {
		t.Fatalf("model overstates shared capabilities: %s", raw)
	}
	reopened := New(ctx, s.db, s.accounts, s.provider)
	defer reopened.Close()
	catalog, err := reopened.GroupCatalog(ctx, 1, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(catalog)
	if !strings.Contains(string(raw), `"synthetic-hidden":{"context_window":10000}`) {
		t.Fatalf("hidden browser capability missing after restart: %s", raw)
	}
	// An eligible resource without optional metadata must make the shared detail unknown.
	if err := s.accounts.SaveCatalog(ctx, ids["claude"], 0, []string{"synthetic-shared"}, s.now().Unix(), s.provider.CatalogSource("claude")); err != nil {
		t.Fatal(err)
	}
	models, err = reopened.Models(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(models[0])
	if strings.Contains(string(raw), "context_window") || strings.Contains(string(raw), "supports_tools") {
		t.Fatalf("unknown metadata became a capability promise: %s", raw)
	}
}

func TestPublicCatalogIncludesHiddenRoutesInCapabilityMerge(t *testing.T) {
	ctx := context.Background()
	s, ids := discoveryGateway(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"models":[{"slug":"synthetic-shared","context_window":200000,"supports_images":true}]}`
		if r.URL.Host == "api.anthropic.com" {
			body = `{"data":[{"id":"synthetic-shared","context_window":100000,"supports_images":false}]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	for _, provider := range []string{"codex", "claude"} {
		if _, err := s.AccountCatalog(ctx, ids[provider], true); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := s.accounts.Catalog(ctx, ids["claude"])
	if err != nil {
		t.Fatal(err)
	}
	saved.HiddenModels = []string{"synthetic-shared"}
	if err := s.accounts.SaveCatalogSnapshot(ctx, ids["claude"], 0, saved); err != nil {
		t.Fatal(err)
	}
	if _, err := groups.New(s.db).Save(ctx, 1, groups.Input{Name: "Synthetic hidden route", Enabled: true, AccountIDs: []string{ids["codex"], ids["claude"]}}); err != nil {
		t.Fatal(err)
	}
	models, err := s.Models(ctx, 1, 1)
	if err != nil || len(models) != 1 {
		t.Fatal(models, err)
	}
	raw, _ := json.Marshal(models[0])
	var got map[string]any
	json.Unmarshal(raw, &got)
	if got["context_window"] != float64(100000) || got["supports_images"] != false || models[0].OwnedBy != "openai" {
		t.Fatalf("hidden eligible route overstated client capabilities: %s", raw)
	}
}
