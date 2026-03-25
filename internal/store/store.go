package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"wcfLink/internal/ilink"
	"wcfLink/internal/model"
)

type Store struct {
	db *sql.DB
}

const defaultWebhookMaxAttempts = 5

func New(ctx context.Context, dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) CreateLoginSession(ctx context.Context, session model.LoginSession) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO login_sessions (
  session_id, base_url, qr_code, qr_code_url, status, error, started_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		session.SessionID, session.BaseURL, session.QRCode, session.QRCodeURL, session.Status,
		session.Error, session.StartedAt.UTC(), session.UpdatedAt.UTC(),
	)
	return err
}

func (s *Store) GetLoginSession(ctx context.Context, sessionID string) (model.LoginSession, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT session_id, base_url, qr_code, qr_code_url, status, account_id, ilink_user_id, bot_token,
       error, started_at, updated_at, completed_at
FROM login_sessions
WHERE session_id = ?`, sessionID)
	var session model.LoginSession
	var completedAt sql.NullTime
	err := row.Scan(
		&session.SessionID, &session.BaseURL, &session.QRCode, &session.QRCodeURL, &session.Status,
		&session.AccountID, &session.ILinkUserID, &session.BotToken, &session.Error,
		&session.StartedAt, &session.UpdatedAt, &completedAt,
	)
	if err != nil {
		return model.LoginSession{}, err
	}
	if completedAt.Valid {
		session.CompletedAt = &completedAt.Time
	}
	return session, nil
}

func (s *Store) UpdateLoginSessionStatus(ctx context.Context, sessionID, status, errorText string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE login_sessions
SET status = ?, error = ?, updated_at = ?
WHERE session_id = ?`, status, errorText, time.Now().UTC(), sessionID)
	return err
}

func (s *Store) CompleteLoginSession(ctx context.Context, sessionID string, status ilink.QRStatusResponse) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
UPDATE login_sessions
SET status = ?, account_id = ?, ilink_user_id = ?, bot_token = ?, base_url = ?, updated_at = ?, completed_at = ?
WHERE session_id = ?`,
		status.Status, status.AccountID, status.ILinkUserID, status.BotToken, status.BaseURL, now, now, sessionID,
	)
	if err != nil {
		return err
	}

	baseURL := status.BaseURL
	if baseURL == "" {
		var fallback string
		if err := tx.QueryRowContext(ctx, `SELECT base_url FROM login_sessions WHERE session_id = ?`, sessionID).Scan(&fallback); err == nil {
			baseURL = fallback
		}
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO accounts (
  account_id, base_url, token, ilink_user_id, enabled, login_status, created_at, updated_at
) VALUES (?, ?, ?, ?, 1, 'connected', ?, ?)
ON CONFLICT(account_id) DO UPDATE SET
  base_url = excluded.base_url,
  token = excluded.token,
  ilink_user_id = excluded.ilink_user_id,
  enabled = 1,
  login_status = 'connected',
  last_error = '',
  updated_at = excluded.updated_at`,
		status.AccountID, baseURL, status.BotToken, status.ILinkUserID, now, now,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) ListAccounts(ctx context.Context) ([]model.Account, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT account_id, base_url, token, ilink_user_id, enabled, login_status, last_error,
       get_updates_buf, last_poll_at, last_inbound_at, created_at, updated_at
FROM accounts
ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.Account
	for rows.Next() {
		var item model.Account
		var enabled int
		var lastPollAt sql.NullTime
		var lastInboundAt sql.NullTime
		if err := rows.Scan(
			&item.AccountID, &item.BaseURL, &item.Token, &item.ILinkUserID, &enabled, &item.LoginStatus,
			&item.LastError, &item.GetUpdatesBuf, &lastPollAt, &lastInboundAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		if lastPollAt.Valid {
			item.LastPollAt = &lastPollAt.Time
		}
		if lastInboundAt.Valid {
			item.LastInboundAt = &lastInboundAt.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetAccount(ctx context.Context, accountID string) (model.Account, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT account_id, base_url, token, ilink_user_id, enabled, login_status, last_error,
       get_updates_buf, last_poll_at, last_inbound_at, created_at, updated_at
FROM accounts
WHERE account_id = ?`, accountID)
	var item model.Account
	var enabled int
	var lastPollAt sql.NullTime
	var lastInboundAt sql.NullTime
	err := row.Scan(
		&item.AccountID, &item.BaseURL, &item.Token, &item.ILinkUserID, &enabled, &item.LoginStatus,
		&item.LastError, &item.GetUpdatesBuf, &lastPollAt, &lastInboundAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.Account{}, err
	}
	item.Enabled = enabled == 1
	if lastPollAt.Valid {
		item.LastPollAt = &lastPollAt.Time
	}
	if lastInboundAt.Valid {
		item.LastInboundAt = &lastInboundAt.Time
	}
	return item, nil
}

func (s *Store) DeleteAccount(ctx context.Context, accountID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	statements := []string{
		`DELETE FROM accounts WHERE account_id = ?`,
		`DELETE FROM peer_contexts WHERE account_id = ?`,
		`DELETE FROM login_sessions WHERE account_id = ?`,
		`DELETE FROM webhook_deliveries WHERE account_id = ?`,
	}
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt, accountID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpdateAccountPollState(ctx context.Context, accountID, getUpdatesBuf, loginStatus, lastError string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE accounts
SET get_updates_buf = ?, login_status = ?, last_error = ?, last_poll_at = ?, updated_at = ?
WHERE account_id = ?`, getUpdatesBuf, loginStatus, lastError, time.Now().UTC(), time.Now().UTC(), accountID)
	return err
}

func (s *Store) TouchAccountInbound(ctx context.Context, accountID string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE accounts
SET last_inbound_at = ?, updated_at = ?
WHERE account_id = ?`, now, now, accountID)
	return err
}

func (s *Store) SaveInboundMessage(ctx context.Context, accountID string, msg ilink.WeixinMessage) (model.Event, bool, error) {
	raw, err := json.Marshal(msg)
	if err != nil {
		return model.Event{}, false, err
	}
	eventType := detectEventType(msg)
	bodyText := extractBodyText(msg)
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Event{}, false, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO events (
  account_id, direction, event_type, from_user_id, to_user_id, message_id, context_token, body_text, raw_json, created_at
) VALUES (?, 'inbound', ?, ?, ?, ?, ?, ?, ?, ?)`,
		accountID, eventType, msg.FromUserID, msg.ToUserID, msg.MessageID, msg.ContextToken, bodyText, string(raw), now,
	)
	if err != nil {
		return model.Event{}, false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return model.Event{}, false, err
	}

	event := model.Event{
		AccountID:    accountID,
		Direction:    "inbound",
		EventType:    eventType,
		FromUserID:   msg.FromUserID,
		ToUserID:     msg.ToUserID,
		MessageID:    msg.MessageID,
		ContextToken: msg.ContextToken,
		BodyText:     bodyText,
		RawJSON:      string(raw),
		CreatedAt:    now,
	}
	inserted := rowsAffected > 0
	if inserted {
		eventID, err := result.LastInsertId()
		if err != nil {
			return model.Event{}, false, err
		}
		event.ID = eventID
	} else if msg.MessageID != 0 {
		row := tx.QueryRowContext(ctx, `
SELECT id, created_at
FROM events
WHERE account_id = ? AND direction = 'inbound' AND message_id = ?`,
			accountID, msg.MessageID,
		)
		if err := row.Scan(&event.ID, &event.CreatedAt); err != nil {
			return model.Event{}, false, err
		}
	}

	if stringsNotEmpty(msg.FromUserID, msg.ContextToken) {
		_, err = tx.ExecContext(ctx, `
INSERT INTO peer_contexts (account_id, peer_user_id, context_token, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(account_id, peer_user_id) DO UPDATE SET
  context_token = excluded.context_token,
  updated_at = excluded.updated_at`, accountID, msg.FromUserID, msg.ContextToken, now)
		if err != nil {
			return model.Event{}, false, err
		}
	}

	_, err = tx.ExecContext(ctx, `
UPDATE accounts
SET last_inbound_at = ?, updated_at = ?, last_error = '', login_status = 'connected'
WHERE account_id = ?`, now, now, accountID)
	if err != nil {
		return model.Event{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return model.Event{}, false, err
	}
	return event, inserted, nil
}

func (s *Store) GetPeerContext(ctx context.Context, accountID, peerUserID string) (model.PeerContext, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT account_id, peer_user_id, context_token, updated_at
FROM peer_contexts
WHERE account_id = ? AND peer_user_id = ?`, accountID, peerUserID)
	var item model.PeerContext
	if err := row.Scan(&item.AccountID, &item.PeerUserID, &item.ContextToken, &item.UpdatedAt); err != nil {
		return model.PeerContext{}, err
	}
	return item, nil
}

func (s *Store) CreateOutboundEvent(ctx context.Context, accountID, toUserID, contextToken, bodyText, rawJSON string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO events (
  account_id, direction, event_type, to_user_id, context_token, body_text, raw_json, created_at
) VALUES (?, 'outbound', 'text', ?, ?, ?, ?, ?)`,
		accountID, toUserID, contextToken, bodyText, rawJSON, time.Now().UTC(),
	)
	return err
}

func (s *Store) CreateOutboundMediaEvent(ctx context.Context, accountID, toUserID, contextToken, eventType, bodyText, rawJSON string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO events (
  account_id, direction, event_type, to_user_id, context_token, body_text, raw_json, created_at
) VALUES (?, 'outbound', ?, ?, ?, ?, ?, ?)`,
		accountID, eventType, toUserID, contextToken, bodyText, rawJSON, time.Now().UTC(),
	)
	return err
}

func (s *Store) EnqueueWebhookDelivery(ctx context.Context, event model.Event, payload []byte) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO webhook_deliveries (
  event_id, account_id, event_type, from_user_id, to_user_id, message_id, body_text, payload_json,
  status, attempt_count, max_attempts, next_attempt_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?, ?, ?, ?)
ON CONFLICT(event_id) DO NOTHING`,
		event.ID, event.AccountID, event.EventType, event.FromUserID, event.ToUserID, event.MessageID,
		event.BodyText, string(payload), defaultWebhookMaxAttempts, now, now, now,
	)
	return err
}

func (s *Store) ListDueWebhookDeliveries(ctx context.Context, limit int) ([]model.WebhookDelivery, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, event_id, account_id, event_type, from_user_id, to_user_id, message_id, body_text,
       payload_json, status, attempt_count, max_attempts, last_error, last_http_status, last_webhook_url,
       next_attempt_at, last_attempt_at, delivered_at, dead_letter_at, created_at, updated_at
FROM webhook_deliveries
WHERE status IN ('pending', 'retrying') AND next_attempt_at <= ?
ORDER BY next_attempt_at ASC, id ASC
LIMIT ?`, time.Now().UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.WebhookDelivery, 0)
	for rows.Next() {
		item, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkWebhookDeliveryDelivered(ctx context.Context, id int64, webhookURL string, statusCode int) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE webhook_deliveries
SET status = 'delivered',
    attempt_count = attempt_count + 1,
    last_error = '',
    last_http_status = ?,
    last_webhook_url = ?,
    last_attempt_at = ?,
    delivered_at = ?,
    updated_at = ?
WHERE id = ?`, statusCode, webhookURL, now, now, now, id)
	return err
}

func (s *Store) MarkWebhookDeliveryForRetry(ctx context.Context, id int64, webhookURL string, statusCode int, errText string, nextAttemptAt time.Time) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE webhook_deliveries
SET status = 'retrying',
    attempt_count = attempt_count + 1,
    last_error = ?,
    last_http_status = ?,
    last_webhook_url = ?,
    last_attempt_at = ?,
    next_attempt_at = ?,
    updated_at = ?
WHERE id = ?`, errText, statusCode, webhookURL, now, nextAttemptAt.UTC(), now, id)
	return err
}

func (s *Store) MarkWebhookDeliveryDead(ctx context.Context, id int64, webhookURL string, statusCode int, errText string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE webhook_deliveries
SET status = 'dead',
    attempt_count = attempt_count + 1,
    last_error = ?,
    last_http_status = ?,
    last_webhook_url = ?,
    last_attempt_at = ?,
    dead_letter_at = ?,
    updated_at = ?
WHERE id = ?`, errText, statusCode, webhookURL, now, now, now, id)
	return err
}

func (s *Store) ListDeadWebhookDeliveries(ctx context.Context, limit int) ([]model.WebhookDelivery, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, event_id, account_id, event_type, from_user_id, to_user_id, message_id, body_text,
       payload_json, status, attempt_count, max_attempts, last_error, last_http_status, last_webhook_url,
       next_attempt_at, last_attempt_at, delivered_at, dead_letter_at, created_at, updated_at
FROM webhook_deliveries
WHERE status = 'dead'
ORDER BY id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.WebhookDelivery, 0)
	for rows.Next() {
		item, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RetryDeadWebhookDelivery(ctx context.Context, id int64) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
UPDATE webhook_deliveries
SET status = 'pending',
    attempt_count = 0,
    last_error = '',
    last_http_status = 0,
    last_attempt_at = NULL,
    delivered_at = NULL,
    dead_letter_at = NULL,
    next_attempt_at = ?,
    updated_at = ?
WHERE id = ? AND status = 'dead'`, now, now, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AddLog(ctx context.Context, level, message, source, metaJSON string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO logs (level, message, source, meta_json, created_at)
VALUES (?, ?, ?, ?, ?)`, level, message, source, metaJSON, time.Now().UTC())
	return err
}

func (s *Store) ListLogs(ctx context.Context, afterID int64, limit int) ([]model.LogEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, level, message, source, meta_json, created_at
FROM logs
WHERE id > ?
ORDER BY id ASC
LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.LogEntry
	for rows.Next() {
		var item model.LogEntry
		if err := rows.Scan(&item.ID, &item.Level, &item.Message, &item.Source, &item.MetaJSON, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListEvents(ctx context.Context, afterID int64, limit int) ([]model.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, account_id, direction, event_type, from_user_id, to_user_id, message_id, context_token, body_text, raw_json, created_at
FROM events
WHERE id > ?
ORDER BY id ASC
LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.Event
	for rows.Next() {
		var item model.Event
		if err := rows.Scan(
			&item.ID, &item.AccountID, &item.Direction, &item.EventType, &item.FromUserID, &item.ToUserID,
			&item.MessageID, &item.ContextToken, &item.BodyText, &item.RawJSON, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`CREATE TABLE IF NOT EXISTS login_sessions (
			session_id TEXT PRIMARY KEY,
			base_url TEXT NOT NULL,
			qr_code TEXT NOT NULL,
			qr_code_url TEXT NOT NULL,
			status TEXT NOT NULL,
			account_id TEXT NOT NULL DEFAULT '',
			ilink_user_id TEXT NOT NULL DEFAULT '',
			bot_token TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			completed_at TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS accounts (
			account_id TEXT PRIMARY KEY,
			base_url TEXT NOT NULL,
			token TEXT NOT NULL,
			ilink_user_id TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			login_status TEXT NOT NULL DEFAULT 'pending',
			last_error TEXT NOT NULL DEFAULT '',
			get_updates_buf TEXT NOT NULL DEFAULT '',
			last_poll_at TIMESTAMP,
			last_inbound_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS peer_contexts (
			account_id TEXT NOT NULL,
			peer_user_id TEXT NOT NULL,
			context_token TEXT NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			PRIMARY KEY (account_id, peer_user_id)
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id TEXT NOT NULL,
			direction TEXT NOT NULL,
			event_type TEXT NOT NULL,
			from_user_id TEXT NOT NULL DEFAULT '',
			to_user_id TEXT NOT NULL DEFAULT '',
			message_id INTEGER NOT NULL DEFAULT 0,
			context_token TEXT NOT NULL DEFAULT '',
			body_text TEXT NOT NULL DEFAULT '',
			raw_json TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_events_account_message_inbound
		 ON events(account_id, direction, message_id)
		 WHERE direction = 'inbound' AND message_id != 0;`,
		`CREATE TABLE IF NOT EXISTS logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			level TEXT NOT NULL,
			message TEXT NOT NULL,
			source TEXT NOT NULL,
			meta_json TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS webhook_deliveries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL UNIQUE,
			account_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			from_user_id TEXT NOT NULL DEFAULT '',
			to_user_id TEXT NOT NULL DEFAULT '',
			message_id INTEGER NOT NULL DEFAULT 0,
			body_text TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL,
			status TEXT NOT NULL,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 5,
			last_error TEXT NOT NULL DEFAULT '',
			last_http_status INTEGER NOT NULL DEFAULT 0,
			last_webhook_url TEXT NOT NULL DEFAULT '',
			next_attempt_at TIMESTAMP NOT NULL,
			last_attempt_at TIMESTAMP,
			delivered_at TIMESTAMP,
			dead_letter_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_status_due
		 ON webhook_deliveries(status, next_attempt_at, id);`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func scanWebhookDelivery(scanner interface {
	Scan(dest ...any) error
}) (model.WebhookDelivery, error) {
	var item model.WebhookDelivery
	var lastAttemptAt sql.NullTime
	var deliveredAt sql.NullTime
	var deadLetterAt sql.NullTime
	err := scanner.Scan(
		&item.ID, &item.EventID, &item.AccountID, &item.EventType, &item.FromUserID, &item.ToUserID,
		&item.MessageID, &item.BodyText, &item.PayloadJSON, &item.Status, &item.AttemptCount,
		&item.MaxAttempts, &item.LastError, &item.LastHTTPStatus, &item.LastWebhookURL,
		&item.NextAttemptAt, &lastAttemptAt, &deliveredAt, &deadLetterAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.WebhookDelivery{}, err
	}
	if lastAttemptAt.Valid {
		item.LastAttemptAt = &lastAttemptAt.Time
	}
	if deliveredAt.Valid {
		item.DeliveredAt = &deliveredAt.Time
	}
	if deadLetterAt.Valid {
		item.DeadLetterAt = &deadLetterAt.Time
	}
	return item, nil
}

func extractBodyText(msg ilink.WeixinMessage) string {
	return ilink.ExtractBodyText(msg)
}

func detectEventType(msg ilink.WeixinMessage) string {
	return ilink.DetectEventType(msg)
}

func stringsNotEmpty(values ...string) bool {
	for _, value := range values {
		if value == "" {
			return false
		}
	}
	return true
}

var ErrNotFound = errors.New("not found")
