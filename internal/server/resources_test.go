package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestResourceGroupsValidateKindsAndPreserveLegacyBindings(t *testing.T) {
	f := newForwardFixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider IO") })
	h := New(Options{Auth: f.identity, Accounts: f.accounts, Groups: f.groups})
	origin := "http://example.test"
	login := request(h, "POST", "/api/auth/login", origin, map[string]string{"username": "owner-test", "password": "owner pass 42"}, nil)
	owner := login.Result().Cookies()[0]
	rows, _ := f.accounts.List(t.Context())
	subscriptionID := rows[0].ID
	created := request(h, "POST", "/api/channels", origin, map[string]string{"name": "Synthetic API channel", "api_key": "synthetic-key", "base_url": "https://relay.example.test/v1"}, owner)
	var channel struct{ ID string }
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &channel) != nil {
		t.Fatal("channel fixture", created.Code)
	}
	input := map[string]any{"name": "Mixed resources", "enabled": true, "resources": []map[string]string{{"kind": "subscription", "id": subscriptionID}, {"kind": "channel", "id": channel.ID}}, "routing": map[string]any{"preference": "subscription_first", "allow_api_fallback": false}}
	input["preference_boosts"] = map[string]int64{subscriptionID: 125}
	saved := request(h, "POST", "/api/groups", origin, input, owner)
	if saved.Code != 201 {
		t.Fatal("typed resource group", saved.Code, saved.Body.String())
	}
	var group struct {
		SubscriptionCount int64 `json:"subscription_count"`
		ChannelCount      int64 `json:"channel_count"`
		ID                int64
		PreferenceBoosts  map[string]int64    `json:"preference_boosts"`
		AccountIDs        []string            `json:"account_ids"`
		Resources         []map[string]string `json:"resources"`
		Routing           struct {
			Preference string
			Fallback   bool `json:"allow_api_fallback"`
		}
	}
	if json.Unmarshal(saved.Body.Bytes(), &group) != nil || len(group.Resources) != 2 || len(group.AccountIDs) != 2 || group.Routing.Preference != "subscription_first" || group.Routing.Fallback {
		t.Fatal("group resources/routing", saved.Body.String())
	}
	if group.PreferenceBoosts[subscriptionID] != 125 {
		t.Fatal("boost missing", saved.Body.String())
	}
	input["name"] = "Invalid boost"
	for _, boost := range []any{-1, 301, 1.5} {
		input["preference_boosts"] = map[string]any{subscriptionID: boost}
		if result := request(h, "POST", "/api/groups", origin, input, owner); result.Code != 400 {
			t.Fatal("invalid boost accepted", boost, result.Code)
		}
	}
	delete(input, "preference_boosts")
	if group.SubscriptionCount != 1 || group.ChannelCount != 1 {
		t.Fatal("resource composition", saved.Body.String())
	}
	input["name"] = "Invalid types"
	input["resources"] = []map[string]string{{"kind": "subscription", "id": channel.ID}}
	if response := request(h, "POST", "/api/groups", origin, input, owner); response.Code != 400 {
		t.Fatal("API channel accepted as subscription", response.Code)
	}
	input["resources"] = []map[string]string{{"kind": "channel", "id": subscriptionID}}
	if response := request(h, "POST", "/api/groups", origin, input, owner); response.Code != 400 {
		t.Fatal("subscription accepted as channel", response.Code)
	}
	input["resources"] = []map[string]string{{"kind": "channel", "id": channel.ID}, {"kind": "channel", "id": channel.ID}}
	if response := request(h, "POST", "/api/groups", origin, input, owner); response.Code != 400 {
		t.Fatal("duplicate resource accepted", response.Code)
	}
	legacy := map[string]any{"name": "Legacy group", "enabled": true, "account_ids": []string{subscriptionID, channel.ID}}
	if response := request(h, "POST", "/api/groups", origin, legacy, owner); response.Code != 201 {
		t.Fatal("legacy group payload broken", response.Code, response.Body.String())
	}
}
