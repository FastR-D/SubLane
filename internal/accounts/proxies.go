package accounts

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/murongg/SubLane/internal/audit"
	"github.com/murongg/SubLane/internal/storage/db"
)

var (
	ErrProxyInput     = errors.New("invalid_proxy_input")
	ErrProxyNotFound  = errors.New("proxy_not_found")
	ErrProxyInUse     = errors.New("proxy_in_use")
	ErrProxyDuplicate = errors.New("proxy_exists")
	ErrProxyLimit     = errors.New("proxy_limit")
	ErrProxyChanged   = errors.New("proxy_changed")
)

type Proxy struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	AccountCount  int64  `json:"account_count"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	CheckedAt     int64  `json:"checked_at"`
	Reachable     bool   `json:"reachable"`
	ExitIP        string `json:"exit_ip"`
	Country       string `json:"country"`
	Region        string `json:"region"`
	City          string `json:"city"`
	LatencyMS     int64  `json:"latency_ms"`
	CheckError    string `json:"check_error"`
	PruneEligible bool   `json:"prune_eligible"`
}

type ProxyObservation struct {
	Reachable bool
	ExitIP    string
	Country   string
	Region    string
	City      string
	LatencyMS int64
	ErrorCode string
}

func parseProxy(address string) (*url.URL, error) {
	if len(address) == 0 || len(address) > 4096 || strings.TrimSpace(address) != address {
		return nil, ErrProxyInput
	}
	u, err := url.Parse(address)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") || u.Hostname() == "" || u.Port() == "" || u.Opaque != "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, ErrProxyInput
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, ErrProxyInput
	}
	if u.User != nil {
		password, ok := u.User.Password()
		if !ok || u.User.Username() == "" || password == "" {
			return nil, ErrProxyInput
		}
	}
	return u, nil
}

func (s *Service) openProxy(id string, encrypted []byte) (string, error) {
	address, err := s.vault.Open("proxy:"+id, encrypted)
	if err != nil {
		return "", err
	}
	if _, err := parseProxy(string(address)); err != nil {
		return "", ErrProxyInput
	}
	return string(address), nil
}

func (s *Service) proxyURL(ctx context.Context, id string) (string, error) {
	row, err := s.queries.GetProxy(ctx, db.GetProxyParams{ID: id, TenantID: s.tenantID})
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrProxyNotFound
	}
	if err != nil {
		return "", err
	}
	return s.openProxy(row.ID, row.Address)
}

// ProxyURL resolves a workspace-owned proxy for an OAuth exchange before the account exists.
func (s *Service) ProxyURL(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	return s.proxyURL(ctx, id)
}

func (s *Service) ListProxies(ctx context.Context) ([]Proxy, error) {
	rows, err := s.queries.ListProxies(ctx, s.tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]Proxy, 0, len(rows))
	cutoff := s.now().Add(-30 * time.Minute).Unix()
	for _, row := range rows {
		address, err := s.openProxy(row.ID, row.Address)
		if err != nil {
			return nil, err
		}
		u, _ := parseProxy(address)
		result = append(result, Proxy{ID: row.ID, Name: row.Name, Endpoint: u.Scheme + "://" + u.Host, AccountCount: row.AccountCount, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, CheckedAt: row.CheckedAt, Reachable: row.Reachable != 0, ExitIP: row.ExitIp, Country: row.Country, Region: row.Region, City: row.City, LatencyMS: row.LatencyMs, CheckError: row.CheckError, PruneEligible: row.CheckedAt >= cutoff && row.Reachable == 0 && row.CheckError == "connection_failed" && row.AccountCount == 0})
	}
	return result, nil
}

func (s *Service) CreateProxy(ctx context.Context, name, address string) (Proxy, error) {
	name, err := NormalizeName(name)
	if err != nil {
		return Proxy{}, ErrProxyInput
	}
	u, err := parseProxy(address)
	if err != nil {
		return Proxy{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().Unix()
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return Proxy{}, err
	}
	id := hex.EncodeToString(idBytes)
	sealed, err := s.vault.Seal("proxy:"+id, []byte(address))
	if err != nil {
		return Proxy{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Proxy{}, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	count, err := q.CountProxies(ctx, s.tenantID)
	if err != nil {
		return Proxy{}, err
	}
	if count >= 32 {
		return Proxy{}, ErrProxyLimit
	}
	n, err := q.CreateProxy(ctx, db.CreateProxyParams{ID: id, TenantID: s.tenantID, Name: name, Address: sealed, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return Proxy{}, err
	}
	if n == 0 {
		return Proxy{}, ErrProxyDuplicate
	}
	if err := audit.Record(ctx, q, "proxy.create", "proxy", id); err != nil {
		return Proxy{}, err
	}
	if err := tx.Commit(); err != nil {
		return Proxy{}, err
	}
	return Proxy{ID: id, Name: name, Endpoint: u.Scheme + "://" + u.Host, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Service) UpdateProxy(ctx context.Context, id, name, address string) (Proxy, error) {
	name, err := NormalizeName(name)
	if err != nil {
		return Proxy{}, ErrProxyInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Proxy{}, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	row, err := q.GetProxy(ctx, db.GetProxyParams{ID: id, TenantID: s.tenantID})
	if errors.Is(err, sql.ErrNoRows) {
		return Proxy{}, ErrProxyNotFound
	}
	if err != nil {
		return Proxy{}, err
	}
	current, err := s.openProxy(id, row.Address)
	if err != nil {
		return Proxy{}, err
	}
	if address == "" {
		address = current
	}
	u, err := parseProxy(address)
	if err != nil {
		return Proxy{}, err
	}
	rows, err := q.ListProxies(ctx, s.tenantID)
	if err != nil {
		return Proxy{}, err
	}
	for _, candidate := range rows {
		if candidate.ID != id && candidate.Name == name {
			return Proxy{}, ErrProxyDuplicate
		}
	}
	sealed := row.Address
	if address != current {
		sealed, err = s.vault.Seal("proxy:"+id, []byte(address))
		if err != nil {
			return Proxy{}, err
		}
	}
	now := s.now().Unix()
	_, err = q.UpdateProxy(ctx, db.UpdateProxyParams{ID: id, TenantID: s.tenantID, Name: name, Address: sealed, UpdatedAt: now})
	if err != nil {
		return Proxy{}, err
	}
	if address != current {
		// An in-flight discovery through the previous exit must not publish its model snapshot.
		if err := q.InvalidateProxyCatalogs(ctx, &id); err != nil {
			return Proxy{}, err
		}
	}
	if err := audit.Record(ctx, q, "proxy.update", "proxy", id); err != nil {
		return Proxy{}, err
	}
	count, err := q.CountProxyAccounts(ctx, &id)
	if err != nil {
		return Proxy{}, err
	}
	if err := tx.Commit(); err != nil {
		return Proxy{}, err
	}
	result := Proxy{ID: id, Name: name, Endpoint: u.Scheme + "://" + u.Host, AccountCount: count, CreatedAt: row.CreatedAt, UpdatedAt: now}
	if address == current {
		result.CheckedAt, result.Reachable, result.ExitIP = row.CheckedAt, row.Reachable != 0, row.ExitIp
		result.Country, result.Region, result.City = row.Country, row.Region, row.City
		result.LatencyMS, result.CheckError = row.LatencyMs, row.CheckError
	}
	return result, nil
}

func (s *Service) ImportProxies(ctx context.Context, raw string) ([]Proxy, error) {
	if len(raw) == 0 || len(raw) > 140<<10 {
		return nil, ErrProxyInput
	}
	type item struct {
		name, address, endpoint string
		explicit                bool
	}
	items := []item{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(items) == 32 {
			return nil, ErrProxyLimit
		}
		name, address, explicit := strings.Cut(line, "|")
		if !explicit {
			address = name
			name = ""
		}
		address = strings.TrimSpace(address)
		if !strings.Contains(address, "://") {
			address = "http://" + address
		}
		u, err := parseProxy(address)
		if err != nil {
			return nil, err
		}
		if explicit {
			name, err = NormalizeName(name)
			if err != nil {
				return nil, ErrProxyInput
			}
		} else {
			name = u.Host
			if utf8.RuneCountInString(name) > 64 {
				name = string([]rune(name)[:64])
			}
		}
		items = append(items, item{name: name, address: address, endpoint: u.Scheme + "://" + u.Host, explicit: explicit})
	}
	if len(items) == 0 {
		return nil, ErrProxyInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	count, err := q.CountProxies(ctx, s.tenantID)
	if err != nil {
		return nil, err
	}
	if count+int64(len(items)) > 32 {
		return nil, ErrProxyLimit
	}
	existing, err := q.ListProxies(ctx, s.tenantID)
	if err != nil {
		return nil, err
	}
	used := make(map[string]bool, len(existing)+len(items))
	for _, row := range existing {
		used[row.Name] = true
	}
	now := s.now().Unix()
	created := make([]Proxy, 0, len(items))
	for _, entry := range items {
		name := entry.name
		if entry.explicit && used[name] {
			return nil, ErrProxyDuplicate
		}
		if !entry.explicit {
			base := name
			for suffix := 2; used[name]; suffix++ {
				label := "-" + strconv.Itoa(suffix)
				runes := []rune(base)
				if len(runes)+len(label) > 64 {
					runes = runes[:64-len(label)]
				}
				name = string(runes) + label
			}
		}
		used[name] = true
		idBytes := make([]byte, 16)
		if _, err := rand.Read(idBytes); err != nil {
			return nil, err
		}
		id := hex.EncodeToString(idBytes)
		sealed, err := s.vault.Seal("proxy:"+id, []byte(entry.address))
		if err != nil {
			return nil, err
		}
		n, err := q.CreateProxy(ctx, db.CreateProxyParams{ID: id, TenantID: s.tenantID, Name: name, Address: sealed, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, ErrProxyDuplicate
		}
		created = append(created, Proxy{ID: id, Name: name, Endpoint: entry.endpoint, CreatedAt: now, UpdatedAt: now})
	}
	if err := audit.Record(ctx, q, "proxy.import", "proxy", ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Service) ProxyCheckTarget(ctx context.Context, id string) (string, int64, error) {
	row, err := s.queries.GetProxy(ctx, db.GetProxyParams{ID: id, TenantID: s.tenantID})
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrProxyNotFound
	}
	if err != nil {
		return "", 0, err
	}
	address, err := s.openProxy(row.ID, row.Address)
	return address, row.Revision, err
}

func (s *Service) SaveProxyCheck(ctx context.Context, id string, revision int64, result ProxyObservation) (Proxy, error) {
	if result.LatencyMS < 0 || result.LatencyMS > 30000 || len(result.ErrorCode) > 32 ||
		(result.ExitIP != "" && net.ParseIP(result.ExitIP) == nil) || len(result.Country) > 2 || len(result.Region) > 128 || len(result.City) > 128 ||
		strings.IndexFunc(result.Country+result.Region+result.City+result.ErrorCode, unicode.IsControl) >= 0 {
		return Proxy{}, ErrProxyInput
	}
	if !result.Reachable {
		result.ExitIP, result.Country, result.Region, result.City = "", "", "", ""
	}
	reachable := int64(0)
	if result.Reachable {
		reachable = 1
	}
	now := s.now().Unix()
	n, err := s.queries.SaveProxyCheck(ctx, db.SaveProxyCheckParams{ID: id, TenantID: s.tenantID, Revision: revision, CheckedAt: now, Reachable: reachable, ExitIp: result.ExitIP, Country: result.Country, Region: result.Region, City: result.City, LatencyMs: result.LatencyMS, CheckError: result.ErrorCode})
	if err != nil {
		return Proxy{}, err
	}
	if n == 0 {
		if _, err := s.queries.GetProxy(ctx, db.GetProxyParams{ID: id, TenantID: s.tenantID}); errors.Is(err, sql.ErrNoRows) {
			return Proxy{}, ErrProxyNotFound
		} else if err != nil {
			return Proxy{}, err
		}
		return Proxy{}, ErrProxyChanged
	}
	row, err := s.queries.GetProxy(ctx, db.GetProxyParams{ID: id, TenantID: s.tenantID})
	if err != nil {
		return Proxy{}, err
	}
	address, err := s.openProxy(row.ID, row.Address)
	if err != nil {
		return Proxy{}, err
	}
	u, _ := parseProxy(address)
	count, err := s.queries.CountProxyAccounts(ctx, &id)
	if err != nil {
		return Proxy{}, err
	}
	return Proxy{ID: id, Name: row.Name, Endpoint: u.Scheme + "://" + u.Host, AccountCount: count, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, CheckedAt: row.CheckedAt, Reachable: row.Reachable != 0, ExitIP: row.ExitIp, Country: row.Country, Region: row.Region, City: row.City, LatencyMS: row.LatencyMs, CheckError: row.CheckError}, nil
}

func (s *Service) DeleteProxy(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	if _, err := q.GetProxy(ctx, db.GetProxyParams{ID: id, TenantID: s.tenantID}); errors.Is(err, sql.ErrNoRows) {
		return ErrProxyNotFound
	} else if err != nil {
		return err
	}
	count, err := q.CountProxyAccounts(ctx, &id)
	if err != nil {
		return err
	}
	if count != 0 {
		return ErrProxyInUse
	}
	if _, err := q.DeleteProxy(ctx, db.DeleteProxyParams{ID: id, TenantID: s.tenantID}); err != nil {
		return err
	}
	if err := audit.Record(ctx, q, "proxy.delete", "proxy", id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) DeleteFailedProxies(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	// Only recent connection failures are decisive. Lookup failures and bound
	// accounts must remain available for manual review.
	n, err := q.DeleteFailedProxies(ctx, db.DeleteFailedProxiesParams{TenantID: s.tenantID, CheckedSince: s.now().Add(-30 * time.Minute).Unix()})
	if err != nil {
		return 0, err
	}
	if n > 0 {
		if err := audit.Record(ctx, q, "proxy.prune", "proxy", ""); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Service) BindProxy(ctx context.Context, accountID, proxyID string) (Account, error) {
	unlock, err := s.lockCredential(ctx, accountID)
	if err != nil {
		return Account{}, err
	}
	defer unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	row, err := q.GetAccount(ctx, db.GetAccountParams{ID: accountID, TenantID: s.tenantID})
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	if (row.ProxyID == nil && proxyID == "") || (row.ProxyID != nil && *row.ProxyID == proxyID) {
		return s.accountMetadata(row)
	}
	var target *string
	if proxyID != "" {
		if _, err := q.GetProxy(ctx, db.GetProxyParams{ID: proxyID, TenantID: s.tenantID}); errors.Is(err, sql.ErrNoRows) {
			return Account{}, ErrProxyNotFound
		} else if err != nil {
			return Account{}, err
		}
		target = &proxyID
	}
	row.ProxyID = target
	row.UpdatedAt = s.now().Unix()
	if _, err := q.BindAccountProxy(ctx, db.BindAccountProxyParams{ID: accountID, TenantID: s.tenantID, ProxyID: target, UpdatedAt: row.UpdatedAt}); err != nil {
		return Account{}, err
	}
	if err := audit.Record(ctx, q, "account.proxy", "account", accountID); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return s.accountMetadata(row)
}
