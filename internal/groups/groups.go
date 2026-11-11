package groups

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/storage/db"
)

// LegacyID identifies pre-pool conversation hashes; it grants no special access.
const LegacyID int64 = 1
const MaxGroups = 32

var (
	ErrInput       = errors.New("invalid_group_input")
	ErrAllocated   = errors.New("allocation_pool_locked")
	ErrNotFound    = errors.New("group_not_found")
	ErrDuplicate   = errors.New("group_exists")
	ErrLimit       = errors.New("group_limit")
	ErrUnavailable = errors.New("group_unavailable")
	ErrInUse       = errors.New("group_in_use")
)

type Group struct {
	SubscriptionCount int64   `json:"subscription_count"`
	ChannelCount      int64   `json:"channel_count"`
	Routing           Routing `json:"routing"`
	RestrictedModels  bool    `json:"restricted_models"`
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	Enabled           bool    `json:"enabled"`
	// IsDefault is retained for response compatibility and is always false.
	IsDefault    bool  `json:"is_default"`
	CreatedAt    int64 `json:"created_at"`
	UpdatedAt    int64 `json:"updated_at"`
	AccountCount int64 `json:"account_count"`
	MemberCount  int64 `json:"member_count"`
}
type Detail struct {
	Group
	Resources     []Resource `json:"resources"`
	AccountIDs    []string   `json:"account_ids"`
	AllowedModels []string   `json:"allowed_models"`
}
type Choice struct {
	SchemeID     int64  `json:"scheme_id,omitempty"`
	SchemeName   string `json:"scheme_name,omitempty"`
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	AccountCount int64  `json:"account_count"`
}
type Member struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}
type Input struct {
	Resources   []Resource   `json:"resources"`
	Routing     *Routing     `json:"routing,omitempty"`
	ModelPolicy *ModelPolicy `json:"model_policy,omitempty"`
	Name        string       `json:"name"`
	Enabled     bool         `json:"enabled"`
	AccountIDs  []string     `json:"account_ids"`
}
type Service struct {
	connection *sql.DB
	queries    *db.Queries
	tenantID   int64
}

func New(connection *sql.DB) *Service {
	return NewForTenant(connection, 1)
}

func NewForTenant(connection *sql.DB, tenantID int64) *Service {
	return &Service{connection: connection, queries: db.New(connection), tenantID: tenantID}
}

func (s *Service) List(ctx context.Context) ([]Group, error) {
	rows, err := s.queries.ListGroups(ctx, s.tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]Group, 0, len(rows))
	for _, r := range rows {
		result = append(result, Group{SubscriptionCount: r.SubscriptionCount, ChannelCount: r.ChannelCount, Routing: Routing{Preference: r.RoutingPreference, AllowAPIFallback: r.AllowApiFallback != 0, SessionSticky: r.SessionSticky}, RestrictedModels: r.RestrictedModels, ID: r.ID, Name: r.Name, Enabled: r.Enabled, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, AccountCount: r.AccountCount, MemberCount: r.MemberCount})
	}
	return result, nil
}
func (s *Service) Get(ctx context.Context, id int64) (Detail, error) {
	tx, err := s.connection.BeginTx(ctx, nil)
	if err != nil {
		return Detail{}, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	row, err := q.GetTenantGroup(ctx, db.GetTenantGroupParams{ID: id, TenantID: s.tenantID})
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	ids, err := q.ListGroupAccounts(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	count, err := q.CountGroupMembers(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	result := Detail{Group: Group{Routing: Routing{Preference: row.RoutingPreference, AllowAPIFallback: row.AllowApiFallback != 0, SessionSticky: row.SessionSticky}, RestrictedModels: row.RestrictedModels, ID: row.ID, Name: row.Name, Enabled: row.Enabled, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, AccountCount: int64(len(ids)), MemberCount: count}, AccountIDs: ids, Resources: []Resource{}}
	for _, sourceID := range ids {
		source, err := q.GetAccount(ctx, db.GetAccountParams{ID: sourceID, TenantID: s.tenantID})
		if err != nil {
			return Detail{}, err
		}
		result.Resources = append(result.Resources, Resource{ID: sourceID, Kind: resourceKind(source.Provider)})
		if source.Provider == "openai" {
			result.ChannelCount++
		} else {
			result.SubscriptionCount++
		}
	}
	result.AllowedModels, err = q.ListGroupModels(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	return result, tx.Commit()
}
func (s *Service) Save(ctx context.Context, id int64, input Input) (Detail, error) {
	ids, kinds, err := resourceIDs(input)
	if err != nil {
		return Detail{}, err
	}
	input.AccountIDs = ids
	if input.Routing != nil && !input.Routing.valid() {
		return Detail{}, ErrInput
	}
	action := "group.update"
	if id == 0 {
		action = "group.create"
	}
	if input.ModelPolicy != nil {
		normalized, err := input.ModelPolicy.normalized()
		if err != nil {
			return Detail{}, err
		}
		input.ModelPolicy = &normalized
	}
	name := strings.TrimSpace(input.Name)
	if id < 0 || !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 64 || strings.IndexFunc(name, unicode.IsControl) >= 0 || input.AccountIDs == nil || len(input.AccountIDs) > 100 {
		return Detail{}, ErrInput
	}
	tx, err := s.connection.BeginTx(ctx, nil)
	if err != nil {
		return Detail{}, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	if id != 0 {
		if _, err := q.GetTenantGroup(ctx, db.GetTenantGroupParams{ID: id, TenantID: s.tenantID}); errors.Is(err, sql.ErrNoRows) {
			return Detail{}, ErrNotFound
		} else if err != nil {
			return Detail{}, err
		}
	}
	other, err := q.FindGroupName(ctx, db.FindGroupNameParams{Name: name, TenantID: s.tenantID})
	if err == nil && other != id {
		return Detail{}, ErrDuplicate
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Detail{}, err
	}
	seen := make(map[string]bool, len(input.AccountIDs))
	for _, accountID := range input.AccountIDs {
		if seen[accountID] {
			return Detail{}, ErrInput
		}
		seen[accountID] = true
		exists, err := q.GroupAccountExists(ctx, db.GroupAccountExistsParams{AccountID: accountID, TenantID: s.tenantID})
		if err != nil {
			return Detail{}, err
		}
		if !exists {
			return Detail{}, ErrInput
		}
		if kinds != nil {
			row, err := q.GetAccount(ctx, db.GetAccountParams{ID: accountID, TenantID: s.tenantID})
			if err != nil {
				return Detail{}, err
			}
			if kinds[accountID] != resourceKind(row.Provider) {
				return Detail{}, ErrInput
			}
		}
	}
	now := time.Now().Unix()
	if id == 0 {
		count, err := q.CountGroups(ctx, s.tenantID)
		if err != nil {
			return Detail{}, err
		}
		if count >= MaxGroups {
			return Detail{}, ErrLimit
		}
		id, err = q.CreateGroup(ctx, db.CreateGroupParams{TenantID: s.tenantID, Name: name, Enabled: input.Enabled, Now: now})
		if err != nil {
			return Detail{}, err
		}
	} else if err := q.UpdateGroup(ctx, db.UpdateGroupParams{ID: id, TenantID: s.tenantID, Name: name, Enabled: input.Enabled, Now: now}); err != nil {
		return Detail{}, err
	}
	managed := false
	if _, e := q.GetPoolAllocation(ctx, id); e == nil {
		managed = true
		old, e := q.ListGroupAccounts(ctx, id)
		if e != nil {
			return Detail{}, e
		}
		next := slices.Clone(input.AccountIDs)
		slices.Sort(next)
		if !slices.Equal(old, next) {
			return Detail{}, ErrAllocated
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return Detail{}, e
	}
	if !managed {
		// Membership replacement commits atomically: no request can observe a half-edited pool.
		if err := q.ClearGroupAccounts(ctx, id); err != nil {
			return Detail{}, err
		}
		for _, accountID := range input.AccountIDs {
			if err := q.AddGroupAccount(ctx, db.AddGroupAccountParams{GroupID: id, AccountID: accountID}); err != nil {
				if strings.Contains(err.Error(), "allocation_") {
					return Detail{}, ErrAllocated
				}
				return Detail{}, err
			}
		}
	}
	if input.ModelPolicy != nil {
		if err := q.SetGroupModelPolicy(ctx, db.SetGroupModelPolicyParams{ID: id, Restricted: input.ModelPolicy.Restricted}); err != nil {
			return Detail{}, err
		}
		if err := q.ClearGroupModels(ctx, id); err != nil {
			return Detail{}, err
		}
		for _, model := range input.ModelPolicy.Models {
			if err := q.AddGroupModel(ctx, db.AddGroupModelParams{GroupID: id, Model: model}); err != nil {
				return Detail{}, err
			}
		}
	}
	if input.Routing != nil {
		var fallback int64
		if input.Routing.AllowAPIFallback {
			fallback = 1
		}
		if err := q.SetGroupRouting(ctx, db.SetGroupRoutingParams{ID: id, Preference: input.Routing.Preference, AllowApiFallback: fallback, SessionSticky: input.Routing.SessionSticky}); err != nil {
			return Detail{}, err
		}
	}
	if err := audit.Record(ctx, q, action, "group", audit.ID(id)); err != nil {
		return Detail{}, err
	}
	if err := tx.Commit(); err != nil {
		return Detail{}, err
	}
	return s.Get(ctx, id)
}

// Delete retires a group: routing, grants and bindings are removed while history keeps its workspace scope.
// Active keys and allowances must be moved or removed first so no member silently loses access.
func (s *Service) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrNotFound
	}
	tx, err := s.connection.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	if _, err := q.GetTenantGroup(ctx, db.GetTenantGroupParams{ID: id, TenantID: s.tenantID}); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	allocated, err := q.GroupHasAllocation(ctx, id)
	if err != nil {
		return err
	}
	if allocated {
		return ErrAllocated
	}
	keys, err := q.GroupActiveKeyCount(ctx, id)
	if err != nil {
		return err
	}
	if keys > 0 {
		return ErrInUse
	}
	for _, clear := range []func(context.Context, int64) error{q.ClearGroupMembers, q.ClearGroupAccounts, q.ClearGroupModels, q.ClearGroupAffinity} {
		if err := clear(ctx, id); err != nil {
			return err
		}
	}
	now := time.Now().Unix()
	changed, err := q.RetireGroup(ctx, db.RetireGroupParams{ID: id, TenantID: s.tenantID, Now: &now})
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrNotFound
	}
	if err := audit.Record(ctx, q, "group.delete", "group", audit.ID(id)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) Available(ctx context.Context, userID int64) ([]Choice, error) {
	rows, err := s.queries.ListAvailableGroups(ctx, db.ListAvailableGroupsParams{UserID: userID, TenantID: s.tenantID})
	if err != nil {
		return nil, err
	}
	result := make([]Choice, 0, len(rows))
	for _, r := range rows {
		result = append(result, Choice{ID: r.ID, Name: r.Name, AccountCount: r.AccountCount})
	}
	return result, nil
}
func (s *Service) MemberGroups(ctx context.Context, userID int64) ([]int64, error) {
	exists, err := s.queries.MemberExistsInTenant(ctx, db.MemberExistsInTenantParams{TenantID: s.tenantID, UserID: userID})
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	return s.queries.ListMemberGroups(ctx, db.ListMemberGroupsParams{UserID: userID, TenantID: s.tenantID})
}
func (s *Service) Members(ctx context.Context, groupID int64) ([]Member, error) {
	if _, err := s.queries.GetTenantGroup(ctx, db.GetTenantGroupParams{ID: groupID, TenantID: s.tenantID}); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	rows, err := s.queries.ListPoolMembers(ctx, groupID)
	if err != nil {
		return nil, err
	}
	members := make([]Member, 0, len(rows))
	for _, row := range rows {
		members = append(members, Member{ID: row.ID, Username: row.Username})
	}
	return members, nil
}
func (s *Service) SetMemberGroups(ctx context.Context, userID int64, ids []int64) error {
	if ids == nil || len(ids) > MaxGroups {
		return ErrInput
	}
	tx, err := s.connection.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	exists, err := q.MemberExistsInTenant(ctx, db.MemberExistsInTenantParams{TenantID: s.tenantID, UserID: userID})
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return ErrInput
		}
		seen[id] = true
		if _, err := q.GetTenantGroup(ctx, db.GetTenantGroupParams{ID: id, TenantID: s.tenantID}); errors.Is(err, sql.ErrNoRows) {
			return ErrInput
		} else if err != nil {
			return err
		}
	}
	if err := q.ClearMemberGroups(ctx, db.ClearMemberGroupsParams{UserID: userID, TenantID: s.tenantID}); err != nil {
		return err
	}
	for _, id := range ids {
		if err := q.GrantMemberGroup(ctx, db.GrantMemberGroupParams{GroupID: id, UserID: userID}); err != nil {
			return err
		}
	}
	if err := audit.Record(ctx, q, "member.groups", "member", audit.ID(userID)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) Connection(ctx context.Context, userID int64) (string, error) {
	return s.queries.GroupConnectionStatus(ctx, db.GroupConnectionStatusParams{UserID: userID, TenantID: s.tenantID})
}
