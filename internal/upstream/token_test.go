package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
)

func TestTokenFailurePreservesOnlySafeRefreshDiagnostics(t *testing.T) {
	err := tokenResponseFailure(401, make(http.Header), []byte(`{"error":{"code":"refresh_token_reused","message":"synthetic-private-token"}}`))
	var failure *accounts.RefreshFailure
	if !errors.As(err, &failure) || failure.Status != 401 || failure.Code != "refresh_token_reused" || !errors.Is(err, accounts.ErrReauthorize) {
		t.Fatalf("refresh diagnostics were lost: %v", err)
	}
	if strings.Contains(err.Error(), "synthetic-private-token") {
		t.Fatal("provider response leaked")
	}
}

func TestProviderTokenFailuresRequireExplicitCredentialEvidence(t *testing.T) {
	for _, provider := range []string{"claude", "antigravity"} {
		for _, mode := range []string{"exchange", "refresh"} {
			for _, tc := range []struct {
				name   string
				status int
				body   string
				want   error
			}{
				{"invalid grant", 400, `{"error":"invalid_grant","detail":"synthetic-private"}`, accounts.ErrReauthorize},
				{"revoked token", 401, `{"error":{"code":"refresh_token_invalidated"}}`, accounts.ErrReauthorize},
				{"client configuration", 400, `{"error":"invalid_client"}`, ErrUpstream},
				{"scope configuration", 400, `{"error":"invalid_scope"}`, ErrUpstream},
				{"unknown unauthorized", 401, `{"error":"synthetic_unknown","message":"invalid_grant"}`, ErrUpstream},
				{"proxy error", 401, `<html>synthetic-private</html>`, ErrUpstream},
				{"server error", 503, `{"error":"invalid_grant"}`, accounts.ErrRefresh},
			} {
				t.Run(provider+"/"+mode+"/"+tc.name, func(t *testing.T) {
					client := NewWithTransport(usageTransport(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
					}))
					defer client.Close()
					_, err := providerTokenRequest(client, provider, mode)
					if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "synthetic-private") {
						t.Fatalf("wrong or unsafe token error: %v", err)
					}
				})
			}
		}
	}
}

func TestProviderTokenFailuresPreserveBoundedRetryAfter(t *testing.T) {
	for _, provider := range []string{"claude", "antigravity"} {
		for _, mode := range []string{"exchange", "refresh"} {
			for _, tc := range []struct {
				name, header string
				min, max     int64
			}{
				{"seconds", "120", 120, 120},
				{"bounded", "9999999", 3600, 3600},
				{"negative", "-1", 0, 0},
				{"invalid", "invalid", 0, 0},
				{"date", "", 115, 120},
			} {
				t.Run(provider+"/"+mode+"/"+tc.name, func(t *testing.T) {
					header := tc.header
					if tc.name == "date" {
						header = time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)
					}
					client := NewWithTransport(usageTransport(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {header}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
					}))
					defer client.Close()
					_, err := providerTokenRequest(client, provider, mode)
					var retry *accounts.RefreshError
					if !errors.As(err, &retry) || retry.RetryAfter < tc.min || retry.RetryAfter > tc.max {
						t.Fatalf("upstream retry deadline lost or unbounded: %v", err)
					}
				})
			}
		}
	}
}

func providerTokenRequest(client *Client, provider, mode string) (accounts.Credential, error) {
	if mode == "exchange" {
		return client.ExchangeFor(context.Background(), provider, "synthetic-code", "synthetic-state", "synthetic-verifier")
	}
	c := antigravityFixture()
	c.Provider = provider
	return client.Refresh(context.Background(), c)
}
