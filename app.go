package main

import (
	"encoding/base64"
	"context"
	"log/slog"
	"time"

	coreapp "wcfLink/internal/app"
	"wcfLink/internal/httpapi"
	"wcfLink/internal/model"
)

type AppBridge struct {
	core   *coreapp.App
	logger *slog.Logger
	ctx    context.Context
}

type LoginSessionView struct {
	SessionID   string `json:"session_id"`
	BaseURL     string `json:"base_url"`
	QRCodeURL   string `json:"qr_code_url"`
	Status      string `json:"status"`
	AccountID   string `json:"account_id,omitempty"`
	ILinkUserID string `json:"ilink_user_id,omitempty"`
	Error       string `json:"error,omitempty"`
	StartedAt   string `json:"started_at"`
	UpdatedAt   string `json:"updated_at"`
	CompletedAt string `json:"completed_at,omitempty"`
}

type AccountView struct {
	AccountID     string `json:"account_id"`
	BaseURL       string `json:"base_url"`
	ILinkUserID   string `json:"ilink_user_id,omitempty"`
	Enabled       bool   `json:"enabled"`
	LoginStatus   string `json:"login_status"`
	LastError     string `json:"last_error,omitempty"`
	LastPollAt    string `json:"last_poll_at,omitempty"`
	LastInboundAt string `json:"last_inbound_at,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type EventView struct {
	ID           int64  `json:"id"`
	AccountID    string `json:"account_id"`
	Direction    string `json:"direction"`
	EventType    string `json:"event_type"`
	FromUserID   string `json:"from_user_id,omitempty"`
	ToUserID     string `json:"to_user_id,omitempty"`
	MessageID    int64  `json:"message_id,omitempty"`
	ContextToken string `json:"context_token,omitempty"`
	BodyText     string `json:"body_text,omitempty"`
	RawJSON      string `json:"raw_json"`
	CreatedAt    string `json:"created_at"`
}

type WebhookDeadLetterView struct {
	ID             int64  `json:"id"`
	EventID        int64  `json:"event_id"`
	AccountID      string `json:"account_id"`
	EventType      string `json:"event_type"`
	FromUserID     string `json:"from_user_id,omitempty"`
	ToUserID       string `json:"to_user_id,omitempty"`
	MessageID      int64  `json:"message_id,omitempty"`
	BodyText       string `json:"body_text,omitempty"`
	Status         string `json:"status"`
	AttemptCount   int    `json:"attempt_count"`
	MaxAttempts    int    `json:"max_attempts"`
	LastError      string `json:"last_error,omitempty"`
	LastHTTPStatus int    `json:"last_http_status,omitempty"`
	LastWebhookURL string `json:"last_webhook_url,omitempty"`
	NextAttemptAt  string `json:"next_attempt_at"`
	LastAttemptAt  string `json:"last_attempt_at,omitempty"`
	DeadLetterAt   string `json:"dead_letter_at,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type Overview struct {
	Settings        model.Settings `json:"settings"`
	Accounts        []AccountView  `json:"accounts"`
	Connected       *AccountView   `json:"connected,omitempty"`
	NeedsLogin      bool           `json:"needs_login"`
	SuggestedTarget string         `json:"suggested_target,omitempty"`
}

func NewAppBridge(core *coreapp.App, logger *slog.Logger) *AppBridge {
	return &AppBridge{core: core, logger: logger}
}

func (a *AppBridge) Startup(ctx context.Context) {
	a.ctx = ctx
	if err := a.core.StartBackground(ctx); err != nil {
		a.logger.Error("failed to start background services", "err", err)
	}
}

func (a *AppBridge) Shutdown(_ context.Context) {
	_ = a.core.Shutdown()
}

func (a *AppBridge) GetOverview() (Overview, error) {
	settings, err := a.core.GetSettings(context.Background())
	if err != nil {
		return Overview{}, err
	}
	accounts, err := a.core.ListAccounts(context.Background())
	if err != nil {
		return Overview{}, err
	}
	accountViews := make([]AccountView, 0, len(accounts))
	connectedIndex := -1
	for i := range accounts {
		view := toAccountView(accounts[i])
		accountViews = append(accountViews, view)
		if connectedIndex == -1 && accounts[i].Enabled && accounts[i].LoginStatus == "connected" && accounts[i].Token != "" {
			connectedIndex = len(accountViews) - 1
		}
	}
	var connected *AccountView
	if connectedIndex >= 0 {
		connected = &accountViews[connectedIndex]
	}
	out := Overview{
		Settings:   settings,
		Accounts:   accountViews,
		Connected:  connected,
		NeedsLogin: connected == nil,
	}
	if connected != nil {
		out.SuggestedTarget = connected.ILinkUserID
	}
	return out, nil
}

func (a *AppBridge) StartLogin() (LoginSessionView, error) {
	session, err := a.core.StartLogin(context.Background(), "")
	if err != nil {
		return LoginSessionView{}, err
	}
	return toLoginSessionView(session), nil
}

func (a *AppBridge) GetLoginStatus(sessionID string) (LoginSessionView, error) {
	session, err := a.core.GetLoginStatus(context.Background(), sessionID)
	if err != nil {
		return LoginSessionView{}, err
	}
	return toLoginSessionView(session), nil
}

func (a *AppBridge) GetLoginQRCode(sessionID string) (string, error) {
	session, err := a.core.GetLoginSession(context.Background(), sessionID)
	if err != nil {
		return "", err
	}
	if session.QRCodeURL == "" {
		return "", nil
	}
	png, err := httpapi.GenerateQRCodePNG(session.QRCodeURL)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

func (a *AppBridge) SaveSettings(listenAddr, webhookURL string) (model.Settings, error) {
	return a.core.UpdateSettings(context.Background(), model.Settings{
		ListenAddr: listenAddr,
		WebhookURL: webhookURL,
	})
}

func (a *AppBridge) ListEvents(afterID int64, limit int) ([]EventView, error) {
	events, err := a.core.ListEvents(context.Background(), afterID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]EventView, 0, len(events))
	for _, item := range events {
		out = append(out, toEventView(item))
	}
	return out, nil
}

func (a *AppBridge) ListDeadLetters(limit int) ([]WebhookDeadLetterView, error) {
	items, err := a.core.ListDeadLetters(context.Background(), limit)
	if err != nil {
		return nil, err
	}
	out := make([]WebhookDeadLetterView, 0, len(items))
	for _, item := range items {
		out = append(out, toWebhookDeadLetterView(item))
	}
	return out, nil
}

func (a *AppBridge) SendText(accountID, toUserID, text string) error {
	return a.core.SendText(context.Background(), accountID, toUserID, text, "")
}

func (a *AppBridge) RetryDeadLetter(id int64) error {
	return a.core.RetryDeadLetter(context.Background(), id)
}

func (a *AppBridge) Logout(accountID string) error {
	return a.core.LogoutAccount(context.Background(), accountID)
}

func (a *AppBridge) NowBJ() string {
	loc := time.FixedZone("CST", 8*3600)
	return time.Now().In(loc).Format("2006-01-02 15:04:05")
}

func toLoginSessionView(in model.LoginSession) LoginSessionView {
	return LoginSessionView{
		SessionID:   in.SessionID,
		BaseURL:     in.BaseURL,
		QRCodeURL:   in.QRCodeURL,
		Status:      in.Status,
		AccountID:   in.AccountID,
		ILinkUserID: in.ILinkUserID,
		Error:       in.Error,
		StartedAt:   formatTime(in.StartedAt),
		UpdatedAt:   formatTime(in.UpdatedAt),
		CompletedAt: formatTimePtr(in.CompletedAt),
	}
}

func toAccountView(in model.Account) AccountView {
	return AccountView{
		AccountID:     in.AccountID,
		BaseURL:       in.BaseURL,
		ILinkUserID:   in.ILinkUserID,
		Enabled:       in.Enabled,
		LoginStatus:   in.LoginStatus,
		LastError:     in.LastError,
		LastPollAt:    formatTimePtr(in.LastPollAt),
		LastInboundAt: formatTimePtr(in.LastInboundAt),
		CreatedAt:     formatTime(in.CreatedAt),
		UpdatedAt:     formatTime(in.UpdatedAt),
	}
}

func toEventView(in model.Event) EventView {
	return EventView{
		ID:           in.ID,
		AccountID:    in.AccountID,
		Direction:    in.Direction,
		EventType:    in.EventType,
		FromUserID:   in.FromUserID,
		ToUserID:     in.ToUserID,
		MessageID:    in.MessageID,
		ContextToken: in.ContextToken,
		BodyText:     in.BodyText,
		RawJSON:      in.RawJSON,
		CreatedAt:    formatTime(in.CreatedAt),
	}
}

func toWebhookDeadLetterView(in model.WebhookDelivery) WebhookDeadLetterView {
	return WebhookDeadLetterView{
		ID:             in.ID,
		EventID:        in.EventID,
		AccountID:      in.AccountID,
		EventType:      in.EventType,
		FromUserID:     in.FromUserID,
		ToUserID:       in.ToUserID,
		MessageID:      in.MessageID,
		BodyText:       in.BodyText,
		Status:         in.Status,
		AttemptCount:   in.AttemptCount,
		MaxAttempts:    in.MaxAttempts,
		LastError:      in.LastError,
		LastHTTPStatus: in.LastHTTPStatus,
		LastWebhookURL: in.LastWebhookURL,
		NextAttemptAt:  formatTime(in.NextAttemptAt),
		LastAttemptAt:  formatTimePtr(in.LastAttemptAt),
		DeadLetterAt:   formatTimePtr(in.DeadLetterAt),
		CreatedAt:      formatTime(in.CreatedAt),
		UpdatedAt:      formatTime(in.UpdatedAt),
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func formatTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}
