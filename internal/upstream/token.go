package upstream

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/murongg/SubLane/internal/accounts"
)

func tokenFailure(response *http.Response) error {
	var raw []byte
	if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnauthorized {
		var err error
		raw, err = readBounded(response.Body, 128<<10)
		if err != nil {
			return ErrUpstream
		}
	}
	return tokenResponseFailure(response.StatusCode, response.Header, raw)
}

func tokenResponseFailure(status int, headers http.Header, raw []byte) (failure error) {
	code := "unknown"
	defer func() {
		failure = &accounts.RefreshFailure{Status: status, Code: accounts.RefreshFailureCode(code), Err: failure}
	}()
	if status == http.StatusBadRequest || status == http.StatusUnauthorized {
		var value struct {
			Error json.RawMessage `json:"error"`
			Code  string          `json:"code"`
		}
		if json.Unmarshal(raw, &value) == nil {
			code = ""
			if json.Unmarshal(value.Error, &code) != nil {
				var detail struct {
					Code string `json:"code"`
				}
				if json.Unmarshal(value.Error, &detail) == nil {
					code = detail.Code
				}
			}
			// Some OAuth servers put the specific rotation reason beside a generic invalid_grant.
			switch value.Code {
			case "refresh_token_reused", "refresh_token_invalidated", "refresh_token_expired":
				code = value.Code
			}
			// HTTP status alone can also describe a proxy or shared OAuth client failure.
			// Match only explicit credential codes, never free-form upstream messages.
			switch code {
			case "invalid_grant", "invalid_refresh_token", "refresh_token_reused", "refresh_token_invalidated", "refresh_token_expired", "token_expired", "app_session_terminated":
				return accounts.ErrReauthorize
			}
		}
	}
	if status == http.StatusTooManyRequests || status >= 500 {
		code = "upstream_unavailable"
		if status == http.StatusTooManyRequests {
			code = "rate_limited"
		}
		var seconds int64
		value := headers.Get("Retry-After")
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			seconds = n
		} else if until, err := http.ParseTime(value); err == nil {
			seconds = int64(time.Until(until).Seconds()) + 1
		}
		return &accounts.RefreshError{RetryAfter: min(3600, max(0, seconds))}
	}
	return ErrUpstream
}
