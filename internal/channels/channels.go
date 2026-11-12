// Package channels owns the API-channel management contract over shared encrypted execution resources.
package channels

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/murongg/SubLane/internal/accounts"
)

var ErrNotFound = errors.New("channel_not_found")

type Channel struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Protocol       string `json:"protocol"`
	Platform       string `json:"platform"`
	BaseURL        string `json:"base_url"`
	Enabled        bool   `json:"enabled"`
	Status         string `json:"status"`
	ProxyID        string `json:"proxy_id,omitempty"`
	MaxConcurrency int64  `json:"max_concurrency"`
	GroupCount     *int64 `json:"group_count,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

type Input struct {
	Name     string `json:"name"`
	APIKey   string `json:"api_key"`
	BaseURL  string `json:"base_url"`
	ProxyID  string `json:"proxy_id,omitempty"`
	Platform string `json:"platform,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

type Service struct{ resources *accounts.Service }

func New(resources *accounts.Service) *Service { return &Service{resources: resources} }

func metadata(a accounts.Account) Channel {
	status := a.Status
	if status == "reauth_required" {
		status = "key_required"
	}
	return Channel{ID: a.ID, Name: a.Name, Protocol: a.APIProtocol, Platform: a.APIPlatform, BaseURL: a.BaseURL, Enabled: a.Enabled, Status: status, ProxyID: a.ProxyID, MaxConcurrency: a.MaxConcurrency, GroupCount: a.GroupCount, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

func (s *Service) List(ctx context.Context) ([]Channel, error) {
	rows, err := s.resources.List(ctx)
	if err != nil {
		return nil, err
	}
	result := []Channel{}
	for _, row := range rows {
		if row.Provider == "openai" {
			result = append(result, metadata(row))
		}
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, id string) (Channel, error) {
	row, err := s.resources.Get(ctx, id)
	if errors.Is(err, accounts.ErrNotFound) || err == nil && row.Provider != "openai" {
		return Channel{}, ErrNotFound
	}
	return metadata(row), err
}

func (s *Service) Save(ctx context.Context, id string, input Input) (Channel, error) {
	if id != "" {
		if _, err := s.Get(ctx, id); err != nil {
			return Channel{}, err
		}
	}
	raw, err := json.Marshal(map[string]string{"api_key": input.APIKey, "base_url": input.BaseURL, "platform": input.Platform, "protocol": input.Protocol})
	if err != nil {
		return Channel{}, accounts.ErrInput
	}
	row, err := s.resources.ImportProviderWithProxy(ctx, "openai", input.Name, raw, id, input.ProxyID)
	return metadata(row), err
}

func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) (Channel, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Channel{}, err
	}
	row, err := s.resources.SetEnabled(ctx, id, enabled)
	return metadata(row), err
}

func (s *Service) BindProxy(ctx context.Context, id, proxy string) (Channel, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Channel{}, err
	}
	row, err := s.resources.BindProxy(ctx, id, proxy)
	return metadata(row), err
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return s.resources.Delete(ctx, id)
}

// Update renames a channel and optionally replaces its key while keeping the channel ID.
func (s *Service) Update(ctx context.Context, id string, input Input) (Channel, error) {
	// UpdateAPIChannel rejects missing and non-channel resources itself.
	row, err := s.resources.UpdateAPIChannel(ctx, id, accounts.APIChannelUpdate{Name: input.Name, APIKey: input.APIKey, BaseURL: input.BaseURL, Platform: input.Platform, Protocol: input.Protocol})
	if errors.Is(err, accounts.ErrNotFound) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, err
	}
	// Group counts and limits are not part of the credential row; reread the full metadata.
	return s.Get(ctx, row.ID)
}
