// Package audit stores bounded management metadata. It never accepts request bodies or credentials.
package audit

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/murongg/SubLane/internal/pagination"
	"github.com/murongg/SubLane/internal/storage/db"
)

var ErrInput = errors.New("invalid_audit_input")

type Actor struct {
	ID                     int64
	TenantID               int64
	Username, Role, Source string
}
type actorKey struct{}

func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}
func ID(id int64) string { return strconv.FormatInt(id, 10) }

type Event struct {
	ID         int64  `json:"id"`
	ActorID    int64  `json:"actor_id"`
	ActorName  string `json:"actor_name"`
	ActorRole  string `json:"actor_role"`
	Source     string `json:"source"`
	Action     string `json:"action"`
	Resource   string `json:"resource"`
	ResourceID string `json:"resource_id"`
	Outcome    string `json:"outcome"`
	HTTPStatus *int64 `json:"http_status"`
	CreatedAt  int64  `json:"created_at"`
}
type Filter struct {
	BeforeID          int64
	Page, Limit       int64
	Resource, Outcome string
}
type Page struct {
	pagination.Metadata
	Events     []Event `json:"events"`
	NextCursor int64   `json:"next_cursor"`
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

var actions = map[string]string{
	"allocation.save": "allocation", "allocation.settle": "allocation", "allocation.reconcile": "allocation", "allocation.delete": "allocation",
	"backup.export": "backup", "backup.prepare": "backup", "backup.verify": "backup",
	"settings.update": "settings",
	"settings.test":   "settings",
	"updates.request": "settings",
	"key.reveal":      "key", "key.create": "key", "key.update": "key", "key.revoke": "key",
	"group.create": "group", "group.update": "group", "group.delete": "group",
	"member.budget": "member", "member.budget_settle": "member", "member.create": "member", "member.update": "member", "member.groups": "member", "member.limits": "member", "member.password": "member",
	"member.remove":     "member",
	"invitation.create": "invitation",
	"user.password":     "user", "user.recover": "user",
	"user.totp_enable": "user", "user.totp_disable": "user", "user.recovery_codes": "user", "user.passkey_add": "user", "user.passkey_delete": "user",
	"account.create": "account", "account.authorize": "account", "account.update": "account", "account.delete": "account", "account.concurrency": "account", "account.resume": "account",
	"account.proxy": "account", "proxy.create": "proxy", "proxy.update": "proxy", "proxy.delete": "proxy", "proxy.import": "proxy", "proxy.prune": "proxy",
	"account.reset_settings": "account", "account.reset_use": "account",
}
var numericID = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var accountID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func ValidTarget(action, resource, id string) bool {
	expected, ok := actions[action]
	if !ok || expected != resource {
		return false
	}
	if id == "" {
		return true
	}
	if resource == "account" || resource == "proxy" {
		return accountID.MatchString(id)
	}
	if resource == "settings" {
		return id == "codex" || id == "grok" || id == "claude" || id == "timezone" || id == "alerts" || id == "content" || id == "turnstile"
	}
	return numericID.MatchString(id)
}

// Record must use the caller's transaction so a successful mutation cannot outlive its audit record.
// Background refreshes have no actor and are intentionally outside the management audit.
func Record(ctx context.Context, q *db.Queries, action, resource, id string) error {
	return record(ctx, q, action, resource, id, nil)
}
func record(ctx context.Context, q *db.Queries, action, resource, id string, status *int64) error {
	if !ValidTarget(action, resource, id) {
		return ErrInput
	}
	actor, ok := ctx.Value(actorKey{}).(Actor)
	if !ok {
		return nil
	}
	if actor.TenantID <= 0 || actor.ID < 0 || len(actor.Username) > 64 || actor.Username == "" || strings.ContainsAny(actor.Username, "\r\n") || (actor.Source != "user" && actor.Source != "local") || (actor.Role != "admin" && actor.Role != "member") {
		return ErrInput
	}
	outcome := "success"
	if status != nil {
		outcome = "failure"
	}
	now := time.Now().Unix()
	if err := q.CreateAuditEvent(ctx, db.CreateAuditEventParams{TenantID: actor.TenantID, ActorID: actor.ID, ActorName: actor.Username, ActorRole: actor.Role, Source: actor.Source, Action: action, Resource: resource, ResourceID: id, Outcome: outcome, HttpStatus: status, CreatedAt: now}); err != nil {
		return err
	}
	return q.PruneAuditEvents(ctx, db.PruneAuditEventsParams{ScopeTenantID: actor.TenantID, Oldest: now - int64((90*24*time.Hour)/time.Second)})
}

// Observation audits disclosure and filesystem preparation without changing live domain records.
// Database mutations must continue using Record with their own transaction.
func (s *Service) Observation(ctx context.Context, action, resource, id string) error {
	tx, err := s.connection.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := Record(ctx, s.queries.WithTx(tx), action, resource, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) Failure(ctx context.Context, action, resource, id string, status int) error {
	if status < 400 || status > 599 {
		return ErrInput
	}
	tx, err := s.connection.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	code := int64(status)
	if err := record(ctx, s.queries.WithTx(tx), action, resource, id, &code); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) List(ctx context.Context, f Filter) (Page, error) {
	page := Page{Events: []Event{}}
	bounds := pagination.Input{Page: f.Page, Limit: f.Limit, Cursor: f.BeforeID}
	if !bounds.Valid() || (f.Outcome != "" && f.Outcome != "success" && f.Outcome != "failure") {
		return page, ErrInput
	}
	switch f.Resource {
	case "", "allocation", "key", "group", "member", "user", "account", "proxy", "settings", "backup", "invitation":
	default:
		return page, ErrInput
	}
	tx, err := s.connection.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	q := s.queries.WithTx(tx)
	if err := q.PruneAuditEvents(ctx, db.PruneAuditEventsParams{ScopeTenantID: s.tenantID, Oldest: time.Now().Add(-90 * 24 * time.Hour).Unix()}); err != nil {
		return page, err
	}
	total, err := q.CountAuditEvents(ctx, db.CountAuditEventsParams{TenantID: s.tenantID, Resource: f.Resource, Outcome: f.Outcome})
	if err != nil {
		return page, err
	}
	page.Metadata = bounds.Resolve(total)
	rows, err := q.ListAuditEvents(ctx, db.ListAuditEventsParams{TenantID: s.tenantID, BeforeID: f.BeforeID, Resource: f.Resource, Outcome: f.Outcome, PageLimit: page.PageSize + 1, PageOffset: page.Offset()})
	if err != nil {
		return page, err
	}
	for _, row := range rows {
		page.Events = append(page.Events, Event{ID: row.ID, ActorID: row.ActorID, ActorName: row.ActorName, ActorRole: row.ActorRole, Source: row.Source, Action: row.Action, Resource: row.Resource, ResourceID: row.ResourceID, Outcome: row.Outcome, HTTPStatus: row.HttpStatus, CreatedAt: row.CreatedAt})
	}
	if int64(len(page.Events)) > page.PageSize {
		page.Events = page.Events[:page.PageSize]
		page.NextCursor = page.Events[len(page.Events)-1].ID
	}
	return page, tx.Commit()
}
