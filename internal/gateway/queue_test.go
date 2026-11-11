package gateway

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/murongg/SubLane/internal/groups"
)

func TestImmediateAdmissionRechecksKeyAndMemberValidity(t *testing.T) {
	for _, change := range []string{"disabled", "revoked", "expired", "member_disabled"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			s, _ := codexGateway(t, transportFunc(func(*http.Request) (*http.Response, error) {
				t.Error("invalid admission reached upstream")
				return syntheticStream(), nil
			}))
			result, err := s.db.ExecContext(ctx, "INSERT INTO api_keys(user_id,group_id,name,prefix,token_hash,created_at) VALUES(1,1,'synthetic','sl_fake',randomblob(32),1)")
			if err != nil {
				t.Fatal(err)
			}
			key, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			statement := map[string]string{
				"disabled":        "UPDATE api_keys SET enabled=0",
				"revoked":         "UPDATE api_keys SET revoked_at=1",
				"expired":         "UPDATE api_keys SET expires_at=1",
				"member_disabled": "UPDATE memberships SET enabled=0",
			}[change]
			if _, err := s.db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
			exchange, err := s.Open(WithRequestIdentity(ctx, key, "http"), 1, 1, []byte(`{"model":"synthetic-model","input":"synthetic"}`), nil, Responses)
			if exchange != nil {
				exchange.Body.Close()
			}
			if !errors.Is(err, groups.ErrUnavailable) {
				t.Fatal("request bypassed revoked access", err)
			}
			if s.memberActive[1] != 0 {
				t.Fatal("rejected admission leaked a member lease")
			}
		})
	}
}

func createAdmissionKey(t *testing.T, s *Service, key, member, group int64) {
	t.Helper()
	if _, err := s.db.Exec("INSERT INTO api_keys(id,user_id,group_id,name,prefix,token_hash,created_at) VALUES(?,?,?,'synthetic','sl_fake',randomblob(32),1)", key, member, group); err != nil {
		t.Fatal(err)
	}
}
