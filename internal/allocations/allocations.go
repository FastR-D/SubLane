// Package allocations owns exclusive pool schemes and their accounting.
package allocations

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/murongg/SubLane/internal/accounts"
	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/groups"
	"github.com/murongg/SubLane/internal/pricing"
	"github.com/murongg/SubLane/internal/storage/db"
)

var (
	ErrInput                 = errors.New("invalid_allocation_input")
	ErrNotFound              = errors.New("allocation_not_found")
	ErrPoolConflict          = errors.New("allocation_pool_conflict")
	ErrUnavailable           = errors.New("allocation_unavailable")
	ErrQuota                 = errors.New("allocation_exhausted")
	ErrPending               = errors.New("allocation_pending")
	ErrRisk                  = errors.New("allocation_risk_limit")
	ErrUnpriced              = errors.New("allocation_model_unpriced")
	ErrUnknownWindowOverride = errors.New("allocation_unknown_window_override")
	ErrSettlement            = errors.New("invalid_allocation_settlement")
)

type Share struct {
	UserID int64 `json:"user_id"`
	// Limit is a percentage in hundredths for share mode; otherwise it
	// uses the selected accounting unit's smallest increment. Time-window
	// members use shared limits unless their duration appears in WindowOverrides.
	Limit           int64               `json:"limit"`
	WindowOverrides []MemberWindowLimit `json:"window_overrides,omitempty"`
}

type MemberWindowLimit struct {
	DurationSeconds int64 `json:"duration_seconds"`
	Limit           int64 `json:"limit"`
}

type WindowCondition struct {
	DurationSeconds int64 `json:"duration_seconds"`
	Limit           int64 `json:"limit"`
}

// Rates are micro-USD per million tokens.
type Rate struct {
	Model  string `json:"model"`
	Input  int64  `json:"input"`
	Cached int64  `json:"cached"`
	Output int64  `json:"output"`
	// ImageInput charges image input tokens of image models; zero falls back to Input.
	ImageInput int64 `json:"image_input,omitempty"`
}
type Config struct {
	Mode      string            `json:"mode"`
	Period    string            `json:"period"`
	ResetTime string            `json:"reset_time,omitempty"`
	ResetDay  int               `json:"reset_day,omitempty"`
	Members   []Share           `json:"members"`
	Rates     []Rate            `json:"rates"`
	RatioUnit string            `json:"ratio_unit,omitempty"`
	Windows   []WindowCondition `json:"windows,omitempty"`
	// Total is in tokens or micro-USD according to RatioUnit.
	Total int64 `json:"total,omitempty"`
}

// PriceCoverage is derived on reads because catalogs and prices change independently of rule revisions.
type PriceCoverage struct {
	MissingCatalogPrices []string `json:"missing_catalog_prices,omitempty"`
	UncoveredModels      []string `json:"uncovered_models,omitempty"`
	CatalogUnavailable   bool     `json:"catalog_unavailable,omitempty"`
}
type SchemeInput struct {
	Conditions
	ConfirmReplacement bool `json:"confirm_replacement,omitempty"`
	enabledProvided    bool
	Name               string `json:"name"`
	GroupID            int64  `json:"group_id"`
	Enabled            bool   `json:"enabled"`
	StartNext          bool   `json:"start_next"`
	ApplyNow           bool   `json:"apply_now"` // If true, edit takes effect immediately instead of at period end
	Config             Config `json:"config"`
}
type Revision struct {
	ID                 int64         `json:"id"`
	EffectiveAt        int64         `json:"effective_at"`
	AccountingAnchorAt int64         `json:"accounting_anchor_at"`
	Config             Config        `json:"config"`
	PriceCoverage      PriceCoverage `json:"price_coverage"`
}
type Scheme struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	GroupID         int64  `json:"group_id"`
	GroupName       string `json:"group_name"`
	Enabled         bool   `json:"enabled"`
	CreatedAt       int64  `json:"created_at"`
	EditEffectiveAt int64  `json:"edit_effective_at"`
	Revision
	Next            *Revision       `json:"next"`
	WriteConditions WriteConditions `json:"write_conditions"`
}
type Service struct {
	conn     *sql.DB
	tenantID int64
	now      func() time.Time
	pricing  *pricing.Service
	location func() *time.Location
}

func New(conn *sql.DB) *Service { return NewForTenant(conn, 1) }
func NewForTenant(conn *sql.DB, tenantID int64) *Service {
	return &Service{conn: conn, tenantID: tenantID, now: time.Now, location: func() *time.Location { return time.UTC }}
}
func NewWithPricing(conn *sql.DB, catalog *pricing.Service) *Service {
	return NewForTenantWithPricing(conn, 1, catalog)
}
func NewForTenantWithPricing(conn *sql.DB, tenantID int64, catalog *pricing.Service) *Service {
	return &Service{conn: conn, tenantID: tenantID, now: time.Now, pricing: catalog, location: func() *time.Location { return time.UTC }}
}

func (s *Service) SetLocation(provider func() *time.Location) { s.location = provider }
func validName(v string) bool {
	return v == strings.TrimSpace(v) && utf8.ValidString(v) && utf8.RuneCountInString(v) > 0 && utf8.RuneCountInString(v) <= 64 && strings.IndexFunc(v, unicode.IsControl) < 0
}
func bit(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
func ratioLimit(total, share int64) int64 {
	// Round down so member limits never sum beyond the configured total.
	return total * share / 10000
}
func validMemberLimit(c Config, m Share) bool {
	if m.UserID <= 0 || m.Limit < 0 || m.Limit > 1_000_000_000_000 {
		return false
	}
	switch c.Mode {
	case "ratio":
		if m.Limit <= 0 || m.Limit > 10000 || len(m.WindowOverrides) != 0 {
			return false
		}
		if c.Period == "durations" {
			for _, window := range c.Windows {
				if ratioLimit(window.Limit, m.Limit) <= 0 {
					return false
				}
			}
			return true
		}
		return ratioLimit(c.Total, m.Limit) > 0
	case "windows":
		if m.Limit != 0 {
			return false
		}
		seen := map[int64]bool{}
		for _, override := range m.WindowOverrides {
			if seen[override.DurationSeconds] || override.Limit < 0 || override.Limit > 1_000_000_000_000 {
				return false
			}
			seen[override.DurationSeconds] = true
		}
		return true
	default:
		return m.Limit > 0 && len(m.WindowOverrides) == 0
	}
}
func normalize(c *Config) error {
	// Match the gateway's pool catalog limit: automatic pricing may include every supported model.
	if (c.Mode != "tokens" && c.Mode != "amount" && c.Mode != "ratio" && c.Mode != "windows") || len(c.Members) == 0 || len(c.Members) > 100 || len(c.Rates) > 4096 {
		return ErrInput
	}
	if !validPeriod(*c) {
		return ErrInput
	}
	if c.Mode == "ratio" {
		if (c.RatioUnit != "tokens" && c.RatioUnit != "amount") || (c.Period == "durations" && c.Total != 0) || (c.Period != "durations" && (c.Total <= 0 || c.Total > 1_000_000_000_000)) {
			return ErrInput
		}
		if c.RatioUnit == "tokens" && len(c.Rates) > 0 {
			return ErrInput
		}
	} else if c.Total != 0 || c.RatioUnit != "" {
		return ErrInput
	}
	if c.Period == "durations" {
		if len(c.Windows) == 0 || len(c.Windows) > 8 {
			return ErrInput
		}
		seen := map[int64]bool{}
		for _, condition := range c.Windows {
			if condition.DurationSeconds < 3600 || condition.DurationSeconds > 365*86400 || condition.DurationSeconds%3600 != 0 || seen[condition.DurationSeconds] || (condition.Limit < 0 || c.Mode == "ratio" && condition.Limit == 0) || condition.Limit > 1_000_000_000_000 {
				return ErrInput
			}
			seen[condition.DurationSeconds] = true
		}
		for _, member := range c.Members {
			for _, override := range member.WindowOverrides {
				if !seen[override.DurationSeconds] {
					return ErrUnknownWindowOverride
				}
			}
		}
	} else if len(c.Windows) > 0 {
		return ErrInput
	}
	seen := map[int64]bool{}
	var total int64
	for _, m := range c.Members {
		if seen[m.UserID] || !validMemberLimit(*c, m) {
			return ErrInput
		}
		seen[m.UserID] = true
		total += m.Limit
	}
	if c.Mode == "ratio" && total > 10000 {
		return ErrInput
	}
	if (c.Mode == "amount" || c.Mode == "windows" || c.Mode == "ratio" && c.RatioUnit == "amount") && len(c.Rates) == 0 {
		return ErrInput
	}
	models := map[string]bool{}
	for i, r := range c.Rates {
		_, model := groups.SplitModel(r.Model)
		if model == "" || len(model) > 128 || models[model] || strings.ContainsAny(model, " \t\n*") {
			return ErrInput
		}
		models[model] = true
		c.Rates[i].Model = model
		if r.Input <= 0 || r.Cached < 0 || r.Output <= 0 || r.ImageInput < 0 || r.Input > 1_000_000_000 || r.Cached > 1_000_000_000 || r.Output > 1_000_000_000 || r.ImageInput > 1_000_000_000 {
			return ErrInput
		}
	}
	if c.Rates == nil {
		c.Rates = []Rate{}
	}
	slices.SortFunc(c.Members, func(a, b Share) int {
		if a.UserID < b.UserID {
			return -1
		}
		if a.UserID > b.UserID {
			return 1
		}
		return 0
	})
	return nil
}
func Current(ctx context.Context, q *db.Queries, id, now int64) (Revision, error) {
	r, err := q.CurrentAllocationRevision(ctx, db.CurrentAllocationRevisionParams{SchemeID: id, EffectiveAt: now})
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, ErrUnavailable
	}
	if err != nil {
		return Revision{}, err
	}
	return storedRevision(r)
}
func decodeRevision(id, at int64, raw string) (Revision, error) {
	r := Revision{ID: id, EffectiveAt: at, AccountingAnchorAt: at}
	if err := json.Unmarshal([]byte(raw), &r.Config); err != nil {
		return r, err
	}
	if !validPeriod(r.Config) {
		return r, ErrInput
	}
	return r, nil
}
func storedRevision(row db.AllocationRevision) (Revision, error) {
	r, err := decodeRevision(row.ID, row.EffectiveAt, row.Config)
	r.AccountingAnchorAt = row.AccountingAnchorAt
	return r, err
}

func (s *Service) Schemes(ctx context.Context) ([]Scheme, error) {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := db.New(tx)
	rows, err := q.ListAllocationSchemes(ctx, s.tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]Scheme, 0, len(rows))
	now := s.now().Unix()
	needsCoverage := false
	for _, r := range rows {
		v, err := s.readSchemeCore(ctx, q, r.ID, now)
		if err != nil {
			return nil, err
		}
		needsCoverage = needsCoverage || schemeNeedsPriceCoverage(v)
		out = append(out, v)
	}
	if !needsCoverage || s.pricing == nil {
		return out, nil
	}
	catalogs, err := loadAllocationPoolCatalogs(ctx, q, s.tenantID)
	if err != nil {
		return nil, err
	}
	for index := range out {
		catalog := catalogs[out[index].GroupID]
		if catalog == nil {
			return nil, fmt.Errorf("allocation pool %d missing from batch catalog", out[index].GroupID)
		}
		if err := s.fillPriceCoverage(ctx, q, &out[index], catalog); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (s *Service) readScheme(ctx context.Context, q *db.Queries, id, now int64) (Scheme, error) {
	out, err := s.readSchemeCore(ctx, q, id, now)
	if err != nil {
		return out, err
	}
	if err := s.fillPriceCoverage(ctx, q, &out, nil); err != nil {
		return out, err
	}
	return out, nil
}
func (s *Service) readSchemeCore(ctx context.Context, q *db.Queries, id, now int64) (Scheme, error) {
	r, err := q.GetTenantAllocationScheme(ctx, db.GetTenantAllocationSchemeParams{ID: id, TenantID: s.tenantID})
	if err != nil {
		return Scheme{}, ErrNotFound
	}
	out := Scheme{ID: r.ID, Name: r.Name, GroupID: r.GroupID, GroupName: r.GroupName, Enabled: r.Enabled == 1, CreatedAt: r.CreatedAt}
	out.Revision, err = Current(ctx, q, id, now)
	if err != nil && !errors.Is(err, ErrUnavailable) {
		return out, err
	}
	if err == nil {
		currentID := out.Revision.ID
		out.WriteConditions.Current = &currentID
		out.EditEffectiveAt = nextEffective(out.Config, now, s.location(), out.AccountingAnchorAt)
	}
	next, e := q.NextAllocationRevision(ctx, db.NextAllocationRevisionParams{SchemeID: id, EffectiveAt: now})
	if e == nil {
		rev, e := storedRevision(next)
		if e != nil {
			return out, e
		}
		out.Next = &rev
		nextID := rev.ID
		out.WriteConditions.Next = &nextID
		out.EditEffectiveAt = rev.EffectiveAt
		if out.Revision.ID == 0 {
			out.Revision = rev
			out.EditEffectiveAt = rev.EffectiveAt
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	return out, nil
}

func schemeNeedsPriceCoverage(scheme Scheme) bool {
	return pricedConfig(scheme.Config) || (scheme.Next != nil && pricedConfig(scheme.Next.Config))
}

func (s *Service) fillPriceCoverage(ctx context.Context, q *db.Queries, scheme *Scheme, catalog *poolModelCatalog) error {
	if s.pricing == nil || !schemeNeedsPriceCoverage(*scheme) {
		return nil
	}
	if catalog == nil {
		loaded, err := loadPoolModelCatalog(ctx, q, scheme.GroupID)
		if err != nil {
			return err
		}
		catalog = &loaded
	}
	scheme.PriceCoverage = s.priceCoverage(*catalog, scheme.Revision)
	if scheme.Next != nil {
		scheme.Next.PriceCoverage = s.priceCoverage(*catalog, *scheme.Next)
	}
	return nil
}
func (in *SchemeInput) UnmarshalJSON(data []byte) error {
	type plain SchemeInput
	var value plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*in = SchemeInput(value)
	_, in.enabledProvided = fields["enabled"]
	return nil
}

func (s *Service) SaveScheme(ctx context.Context, id int64, in SchemeInput) (Scheme, error) {
	if id < 0 || !validName(in.Name) || in.GroupID <= 0 || !validPeriod(in.Config) {
		return Scheme{}, ErrInput
	}
	if in.Config.Mode == "ratio" {
		var sum int64
		for _, m := range in.Config.Members {
			sum += m.Limit
		}
		if sum > 10000 {
			return Scheme{}, ErrInput
		}
	}
	if id > 0 {
		return s.updateScheme(ctx, id, in)
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Scheme{}, err
	}
	defer tx.Rollback()
	q := db.New(tx)
	now := s.now().Unix()
	if _, err := q.GetTenantGroup(ctx, db.GetTenantGroupParams{ID: in.GroupID, TenantID: s.tenantID}); err != nil {
		return Scheme{}, ErrInput
	}
	if err := s.validateRecipients(ctx, q, in.GroupID, in.Config.Members); err != nil {
		return Scheme{}, err
	}
	accounts, err := q.AllocationPoolAccounts(ctx, in.GroupID)
	if err != nil {
		return Scheme{}, err
	}
	if len(accounts) == 0 {
		return Scheme{}, ErrInput
	}
	for _, a := range accounts {
		if a.Shared > 0 {
			return Scheme{}, ErrPoolConflict
		}
	}
	if _, err := s.applyPrices(ctx, q, in.GroupID, &in.Config, nil); err != nil {
		return Scheme{}, err
	}
	if err := normalize(&in.Config); err != nil {
		return Scheme{}, err
	}
	if _, err = q.GetPoolAllocation(ctx, in.GroupID); err == nil {
		return Scheme{}, ErrPoolConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Scheme{}, err
	}
	rows, err := q.ListAllocationSchemes(ctx, s.tenantID)
	if err != nil {
		return Scheme{}, err
	}
	if len(rows) >= 32 {
		return Scheme{}, ErrInput
	}
	effective, err := s.creationTime(in, now)
	if err != nil {
		return Scheme{}, err
	}
	id, err = q.CreateAllocationScheme(ctx, db.CreateAllocationSchemeParams{Name: in.Name, GroupID: in.GroupID, Enabled: bit(in.Enabled), CreatedAt: now})
	if err != nil {
		return Scheme{}, err
	}
	if _, err := appendRevision(ctx, q, id, effective, effective, in.Config); err != nil {
		return Scheme{}, err
	}
	// Bind paused and expired keys too, without changing their lifecycle; later activation must still enforce the allowance.
	for _, member := range in.Config.Members {
		if err := q.BindExistingAllocationKeys(ctx, db.BindExistingAllocationKeysParams{SchemeID: id, GroupID: in.GroupID, UserID: member.UserID}); err != nil {
			return Scheme{}, err
		}
	}
	if err := audit.Record(ctx, q, "allocation.save", "allocation", audit.ID(id)); err != nil {
		return Scheme{}, err
	}
	out, err := s.readScheme(ctx, q, id, now)
	if err != nil {
		return out, err
	}
	if len(in.ExpectedEffectiveAt) > 0 {
		if at, err := s.creationTime(in, s.now().Unix()); err != nil {
			return Scheme{}, err
		} else if at != effective {
			return Scheme{}, ErrEffectiveConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return Scheme{}, err
	}
	return out, nil
}

type poolModelCatalog struct {
	policy        groups.ModelPolicy
	models        []string
	visibleModels map[string]bool
	unavailable   bool
}

// Both loaders sort models after all accounts are aggregated; this step only deduplicates.
func (catalog *poolModelCatalog) addAccount(provider string, snapshot []byte, revision int64, seen map[string]bool) {
	accountCatalog, err := accounts.DecodeCatalog(snapshot, revision)
	if err != nil || accountCatalog.UpdatedAt == 0 {
		catalog.unavailable = true
		return
	}
	if catalog.visibleModels == nil {
		catalog.visibleModels = map[string]bool{}
	}
	for _, model := range accountCatalog.Models {
		_, native := groups.SplitModel(model)
		// The policy needs the account provider to enforce provider-scoped allowlists.
		if !catalog.policy.Allows(provider + "/" + native) {
			continue
		}
		// A permitted visible route wins even when another account listed this model as hidden first.
		if !slices.Contains(accountCatalog.HiddenModels, model) {
			catalog.visibleModels[native] = true
		}
		if !seen[native] {
			seen[native] = true
			catalog.models = append(catalog.models, native)
		}
	}
}

func pricedConfig(config Config) bool {
	return config.Mode == "amount" || config.Mode == "windows" || config.Mode == "ratio" && config.RatioUnit == "amount"
}

// Save and read paths use the same pool model set; warning state itself is never saved in a revision.
func loadPoolModelCatalog(ctx context.Context, q *db.Queries, groupID int64) (poolModelCatalog, error) {
	group, err := q.GetGroup(ctx, groupID)
	if err != nil {
		return poolModelCatalog{}, err
	}
	allowed, err := q.ListGroupModels(ctx, groupID)
	if err != nil {
		return poolModelCatalog{}, err
	}
	catalog := poolModelCatalog{policy: groups.ModelPolicy{Restricted: group.RestrictedModels, Models: allowed}}
	rows, err := q.ListGroupCatalogs(ctx, groupID)
	if err != nil {
		return poolModelCatalog{}, err
	}
	seen := map[string]bool{}
	for _, row := range rows {
		catalog.addAccount(row.Provider, row.ModelsSnapshot, row.ModelsRevision, seen)
	}
	sort.Strings(catalog.models)
	return catalog, nil
}

// One query per relation covers every managed pool in a tenant; exclusive pool IDs never repeat across schemes.
func loadAllocationPoolCatalogs(ctx context.Context, q *db.Queries, tenantID int64) (map[int64]*poolModelCatalog, error) {
	policies, err := q.ListAllocationPoolPolicies(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	catalogs := make(map[int64]*poolModelCatalog, len(policies))
	for _, row := range policies {
		catalogs[row.GroupID] = &poolModelCatalog{policy: groups.ModelPolicy{Restricted: row.RestrictedModels}}
	}
	models, err := q.ListAllocationPoolModels(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, row := range models {
		catalog := catalogs[row.GroupID]
		if catalog == nil {
			return nil, fmt.Errorf("allocation pool %d has no policy", row.GroupID)
		}
		catalog.policy.Models = append(catalog.policy.Models, row.Model)
	}
	accounts, err := q.ListAllocationPoolCatalogs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]map[string]bool, len(catalogs))
	for _, row := range accounts {
		catalog := catalogs[row.GroupID]
		if catalog == nil {
			return nil, fmt.Errorf("allocation pool %d has no policy", row.GroupID)
		}
		if seen[row.GroupID] == nil {
			seen[row.GroupID] = map[string]bool{}
		}
		catalog.addAccount(row.Provider, row.ModelsSnapshot, row.ModelsRevision, seen[row.GroupID])
	}
	for _, catalog := range catalogs {
		sort.Strings(catalog.models)
	}
	return catalogs, nil
}

func (s *Service) priceCoverage(catalog poolModelCatalog, revision Revision) PriceCoverage {
	if !pricedConfig(revision.Config) {
		return PriceCoverage{}
	}
	coverage := PriceCoverage{CatalogUnavailable: catalog.unavailable}
	saved := make(map[string]bool, len(revision.Config.Rates))
	for _, rate := range revision.Config.Rates {
		saved[rate.Model] = true
	}
	for _, model := range catalog.models {
		// Hidden models still need saved prices for admission; only their coverage warnings are omitted.
		if !catalog.visibleModels[model] {
			continue
		}
		if !saved[model] {
			coverage.UncoveredModels = append(coverage.UncoveredModels, model)
		} else if _, ok := s.pricing.Lookup(model); !ok {
			coverage.MissingCatalogPrices = append(coverage.MissingCatalogPrices, model)
		}
	}
	return coverage
}

// Priced revisions use the current catalog, then their previous saved rates when a price disappears.
// Client-supplied rates never override catalog or previously saved prices in production.
func (s *Service) applyPrices(ctx context.Context, q *db.Queries, groupID int64, config *Config, previous []Rate) (*poolModelCatalog, error) {
	if !pricedConfig(*config) {
		config.Rates = []Rate{}
		return nil, nil
	}
	if s.pricing == nil {
		// Production injects a pricing service; explicit rates remain available to test and embedded callers.
		if len(config.Rates) == 0 {
			config.Rates = append([]Rate(nil), previous...)
		}
		if len(config.Rates) > 0 {
			return nil, nil
		}
		return nil, ErrUnpriced
	}
	catalog, err := loadPoolModelCatalog(ctx, q, groupID)
	if err != nil {
		return nil, err
	}
	previousByModel := make(map[string]Rate, len(previous))
	for _, rate := range previous {
		previousByModel[rate.Model] = rate
	}
	models := make(map[string]bool, len(catalog.models))
	for _, model := range catalog.models {
		models[model] = true
	}
	// If a catalog is temporarily unknown, retain existing prices while its models cannot be rediscovered.
	if catalog.unavailable {
		for model := range previousByModel {
			if catalog.policy.Allows(model) {
				models[model] = true
			}
		}
	}
	ids := make([]string, 0, len(models))
	for model := range models {
		ids = append(ids, model)
	}
	sort.Strings(ids)
	config.Rates = make([]Rate, 0, len(ids))
	for _, model := range ids {
		if price, ok := s.pricing.Lookup(model); ok {
			config.Rates = append(config.Rates, Rate{Model: model, Input: price.Input, Cached: price.Cached, Output: price.Output, ImageInput: price.ImageInput})
			continue
		}
		if rate, ok := previousByModel[model]; ok {
			config.Rates = append(config.Rates, rate)
		}
	}
	if len(config.Rates) == 0 {
		return nil, ErrUnpriced
	}
	return &catalog, nil
}
func Cost(r Rate, input, output, cached int64) (int64, error) {
	return CostWithImages(r, input, 0, output, cached)
}

// CostWithImages settles input by modality: imageInput tokens are part of input and use the image rate.
func CostWithImages(r Rate, input, imageInput, output, cached int64) (int64, error) {
	if input < 0 || output < 0 || cached < 0 || imageInput < 0 || cached+imageInput > input || input > 1_000_000_000 || output > 1_000_000_000 {
		return 0, ErrInput
	}
	imageRate := r.ImageInput
	if imageRate == 0 {
		imageRate = r.Input
	}
	// Round only after summing all token categories; even sub-micro charges remain nonzero.
	sum := new(big.Int)
	for _, v := range [][2]int64{{input - cached - imageInput, r.Input}, {imageInput, imageRate}, {cached, r.Cached}, {output, r.Output}} {
		if v[1] < 0 {
			return 0, ErrInput
		}
		sum.Add(sum, new(big.Int).Mul(big.NewInt(v[0]), big.NewInt(v[1])))
	}
	sum.Add(sum, big.NewInt(999999))
	sum.Div(sum, big.NewInt(1000000))
	if !sum.IsInt64() {
		return 0, ErrInput
	}
	return sum.Int64(), nil
}

// Access toggles never depend on upstream availability or rewrite the policy revision.
// ErrInFlight blocks deletion while admitted requests still hold allowance reservations.
var ErrInFlight = errors.New("allocation_in_flight")

// Delete removes an allowance with its revisions, key bindings and budget ledger in one audited
// transaction. Request history and usage aggregates are stored separately and are retained.
// Keys that were bound to it keep their resource group and are no longer budget-limited.
func (s *Service) Delete(ctx context.Context, id int64) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := db.New(tx)
	if _, err := q.GetTenantAllocationScheme(ctx, db.GetTenantAllocationSchemeParams{ID: id, TenantID: s.tenantID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	active, err := q.AllocationHasActiveEntries(ctx, id)
	if err != nil {
		return err
	}
	if active {
		return ErrInFlight
	}
	// Children first: entries reference revisions, and every child references the scheme.
	for _, remove := range []func(context.Context, int64) error{q.DeleteAllocationEntries, q.DeleteAllocationKeys, q.DeleteAllocationRevisions} {
		if err := remove(ctx, id); err != nil {
			return err
		}
	}
	removed, err := q.DeleteAllocationScheme(ctx, id)
	if err != nil {
		return err
	}
	if removed != 1 {
		return ErrNotFound
	}
	if err := audit.Record(ctx, q, "allocation.delete", "allocation", audit.ID(id)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := db.New(tx)
	row, err := q.GetTenantAllocationScheme(ctx, db.GetTenantAllocationSchemeParams{ID: id, TenantID: s.tenantID})
	if err != nil {
		return ErrNotFound
	}
	if err = q.UpdateAllocationScheme(ctx, db.UpdateAllocationSchemeParams{ID: id, Name: row.Name, Enabled: bit(enabled)}); err != nil {
		return err
	}
	if err = audit.Record(ctx, q, "allocation.save", "allocation", audit.ID(id)); err != nil {
		return err
	}
	return tx.Commit()
}
