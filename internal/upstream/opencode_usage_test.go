package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/murongg/SubLane/internal/accounts"
)

func TestOpenCodeGoUsageReadsSpendingWindows(t *testing.T) {
	channel := accounts.Credential{Provider: "openai", APIPlatform: "opencode-go", APIProtocol: "openai", BaseURL: "https://opencode.ai/zen/go/v1", AccessToken: "synthetic-opencode-key"}
	body := `{"usage":{"rolling":{"status":"ok","percent":23,"resetsAt":"2030-01-01T05:00:00.000Z"},"weekly":{"status":"ok","percent":10,"resetsAt":"2030-01-06T00:00:00.000Z"},"monthly":{"status":"ok","percent":100,"resetsAt":"2030-01-31T00:00:00.000Z"}}}`
	calls := 0
	client := NewWithTransport(usageTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.String() != OpenCodeGoUsageURL || r.Header.Get("Authorization") != "Bearer synthetic-opencode-key" {
			t.Error("wrong OpenCode Go usage request", r.URL.String())
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	defer client.Close()
	usage, err := client.Usage(context.Background(), channel)
	if err != nil || len(usage.Limits) != 1 || usage.Limits[0].Name != "" {
		t.Fatal("usage", usage, err)
	}
	limit := usage.Limits[0]
	if limit.LimitReached == nil || !*limit.LimitReached || len(limit.Windows) != 3 {
		t.Fatal("limit", limit)
	}
	want := []struct {
		kind    string
		used    float64
		seconds int64
		reset   int64
	}{{"primary", 23, 5 * 3600, 1893474000}, {"secondary", 10, 7 * 86400, 1893888000}, {"monthly", 100, 0, 1896048000}}
	for i, expected := range want {
		window := limit.Windows[i]
		if window.Kind != expected.kind || window.UsedPercent == nil || *window.UsedPercent != expected.used || window.ResetAt == nil || *window.ResetAt != expected.reset {
			t.Fatal("window", i, window)
		}
		if expected.seconds == 0 && window.WindowSeconds != nil || expected.seconds != 0 && (window.WindowSeconds == nil || *window.WindowSeconds != expected.seconds) {
			t.Fatal("window length", i, window.WindowSeconds)
		}
	}

	for _, invalid := range []string{`{}`, `{"usage":{"rolling":{"percent":-1}}}`, `not json`} {
		body = invalid
		if _, err := client.Usage(context.Background(), channel); !errors.Is(err, ErrResponse) {
			t.Fatal("invalid usage accepted", invalid, err)
		}
	}
	body = `{"usage":{"weekly":{"status":"ok"}}}`
	if usage, err := client.Usage(context.Background(), channel); err != nil || len(usage.Limits) != 1 || len(usage.Limits[0].Windows) != 1 || usage.Limits[0].Windows[0].UsedPercent != nil {
		t.Fatal("missing percentages must stay unknown", usage, err)
	}
	other := channel
	other.APIPlatform = "openrouter"
	if _, err := client.Usage(context.Background(), other); !errors.Is(err, ErrUsageUnsupported) {
		t.Fatal("other channels must not report usage", err)
	}
	// The preset label is not enough: a relay key must never be sent to the official endpoint.
	for _, relay := range []string{"https://relay.example.test/zen/go/v1", "http://opencode.ai/zen/go/v1", "https://opencode.ai.example.test/v1", "https://opencode.ai:8443/v1"} {
		configured := channel
		configured.BaseURL = relay
		before := calls
		if _, err := client.Usage(context.Background(), configured); !errors.Is(err, ErrUsageUnsupported) || calls != before {
			t.Fatal("relay channel reached the official usage endpoint", relay, err)
		}
	}
}

func TestCodexImageModelsJoinOnlyGPTCatalogs(t *testing.T) {
	if got := appendImageModels([]Model{}); len(got) != 0 {
		t.Fatal("empty catalog gained image models", got)
	}
	if got := appendImageModels([]Model{{ID: "synthetic-model"}}); len(got) != 1 {
		t.Fatal("non-GPT catalog gained image models", got)
	}
	got := appendImageModels([]Model{{ID: "gpt-5.4-mini"}, {ID: "gpt-image-2"}})
	if len(got) != 1+len(codexImageModels) {
		t.Fatal("image models were not added exactly once", got)
	}
	// Upstream-advertised entries are kept as observed; only added entries carry the built-in capability.
	for _, model := range got[2:] {
		if !IsImageModel(model.ID) || model.SupportsImages == nil || !*model.SupportsImages {
			t.Fatal("image capability", model)
		}
	}
}
