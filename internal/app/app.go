package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"wcfLink/internal/config"
	"wcfLink/internal/httpapi"
	"wcfLink/internal/ilink"
	"wcfLink/internal/model"
	"wcfLink/internal/store"
	"wcfLink/internal/worker"
)

type App struct {
	cfg     config.Config
	logger  *slog.Logger
	store   *store.Store
	client  *ilink.Client
	pollers *worker.PollerManager
	webhooks *worker.WebhookDispatcher
	server  *http.Server
	runtime *runtimeState
	svc     *service
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	st, err := store.New(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}
	client := ilink.NewClient(cfg.ChannelVersion, cfg.PollTimeout+10*time.Second)
	runtime := newRuntimeState(st, cfg)
	svc := &service{
		cfg:     cfg,
		logger:  logger,
		store:   st,
		client:  client,
		runtime: runtime,
	}
	pollers := worker.NewPollerManager(st, client, logger, svc.HandleInboundMessage)
	webhooks := worker.NewWebhookDispatcher(st, runtime.Settings, logger)
	svc.pollers = pollers
	svc.webhooks = webhooks
	api := httpapi.NewServer(&service{
		cfg:     cfg,
		logger:  logger,
		store:   st,
		client:  client,
		pollers: pollers,
		webhooks: webhooks,
		runtime: runtime,
	}, logger)

	return &App{
		cfg:     cfg,
		logger:  logger,
		store:   st,
		client:  client,
		pollers: pollers,
		webhooks: webhooks,
		runtime: runtime,
		svc:     svc,
		server: &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           api.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.StartBackground(ctx); err != nil {
		return err
	}

	<-ctx.Done()
	a.logger.Info("shutdown requested")
	return a.Shutdown()
}

func (a *App) StartBackground(ctx context.Context) error {
	if err := a.store.Ping(ctx); err != nil {
		return err
	}
	if err := a.pollers.StartEnabledAccounts(ctx); err != nil {
		return err
	}
	a.webhooks.Start(ctx)

	go func() {
		a.logger.Info("http server listening", "addr", a.cfg.ListenAddr)
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Error("http server stopped with error", "err", err)
		}
	}()
	return nil
}

func (a *App) Shutdown() error {
	a.pollers.StopAll()
	a.webhooks.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = a.server.Shutdown(shutdownCtx)
	return a.store.Close()
}

func (a *App) StartLogin(ctx context.Context, baseURL string) (model.LoginSession, error) {
	return a.svc.StartLogin(ctx, baseURL)
}

func (a *App) GetLoginStatus(ctx context.Context, sessionID string) (model.LoginSession, error) {
	return a.svc.GetLoginStatus(ctx, sessionID)
}

func (a *App) GetLoginSession(ctx context.Context, sessionID string) (model.LoginSession, error) {
	return a.svc.GetLoginSession(ctx, sessionID)
}

func (a *App) ListAccounts(ctx context.Context) ([]model.Account, error) {
	return a.svc.ListAccounts(ctx)
}

func (a *App) ListEvents(ctx context.Context, afterID int64, limit int) ([]model.Event, error) {
	return a.svc.ListEvents(ctx, afterID, limit)
}

func (a *App) GetSettings(ctx context.Context) (model.Settings, error) {
	return a.svc.GetSettings(ctx)
}

func (a *App) UpdateSettings(ctx context.Context, settings model.Settings) (model.Settings, error) {
	return a.svc.UpdateSettings(ctx, settings)
}

func (a *App) SendText(ctx context.Context, accountID, toUserID, text, contextToken string) error {
	return a.svc.SendText(ctx, accountID, toUserID, text, contextToken)
}

func (a *App) LogoutAccount(ctx context.Context, accountID string) error {
	return a.svc.LogoutAccount(ctx, accountID)
}

func (a *App) ListDeadLetters(ctx context.Context, limit int) ([]model.WebhookDelivery, error) {
	return a.svc.ListDeadLetters(ctx, limit)
}

func (a *App) RetryDeadLetter(ctx context.Context, id int64) error {
	return a.svc.RetryDeadLetter(ctx, id)
}

type service struct {
	cfg     config.Config
	logger  *slog.Logger
	store   *store.Store
	client  *ilink.Client
	pollers *worker.PollerManager
	webhooks *worker.WebhookDispatcher
	runtime *runtimeState
}

func (s *service) StartLogin(ctx context.Context, baseURL string) (model.LoginSession, error) {
	if baseURL == "" {
		baseURL = s.cfg.DefaultBaseURL
	}
	qr, err := s.client.FetchQRCode(ctx, baseURL)
	if err != nil {
		return model.LoginSession{}, err
	}
	now := time.Now().UTC()
	session := model.LoginSession{
		SessionID: fmt.Sprintf("login_%d", now.UnixNano()),
		BaseURL:   baseURL,
		QRCode:    qr.QRCode,
		QRCodeURL: qr.QRCodeURL,
		Status:    "wait",
		StartedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateLoginSession(ctx, session); err != nil {
		return model.LoginSession{}, err
	}
	return session, nil
}

func (s *service) GetLoginStatus(ctx context.Context, sessionID string) (model.LoginSession, error) {
	session, err := s.store.GetLoginSession(ctx, sessionID)
	if err != nil {
		return model.LoginSession{}, err
	}
	if session.Status == "confirmed" {
		return session, nil
	}
	status, err := s.client.FetchQRCodeStatus(ctx, session.BaseURL, session.QRCode)
	if err != nil {
		_ = s.store.UpdateLoginSessionStatus(context.Background(), sessionID, session.Status, err.Error())
		return model.LoginSession{}, err
	}
	if status.Status == "" {
		return session, nil
	}
	if status.Status == "confirmed" {
		if err := s.store.CompleteLoginSession(ctx, sessionID, status); err != nil {
			return model.LoginSession{}, err
		}
		account, err := s.store.GetAccount(ctx, status.AccountID)
		if err == nil {
			s.pollers.StartAccount(context.Background(), account)
		}
		return s.store.GetLoginSession(ctx, sessionID)
	}
	if err := s.store.UpdateLoginSessionStatus(ctx, sessionID, status.Status, ""); err != nil {
		return model.LoginSession{}, err
	}
	return s.store.GetLoginSession(ctx, sessionID)
}

func (s *service) GetLoginSession(ctx context.Context, sessionID string) (model.LoginSession, error) {
	return s.store.GetLoginSession(ctx, sessionID)
}

func (s *service) ListAccounts(ctx context.Context) ([]model.Account, error) {
	return s.store.ListAccounts(ctx)
}

func (s *service) ListEvents(ctx context.Context, afterID int64, limit int) ([]model.Event, error) {
	return s.store.ListEvents(ctx, afterID, limit)
}

func (s *service) ListLogs(ctx context.Context, afterID int64, limit int) ([]model.LogEntry, error) {
	return s.store.ListLogs(ctx, afterID, limit)
}

func (s *service) ListDeadLetters(ctx context.Context, limit int) ([]model.WebhookDelivery, error) {
	return s.store.ListDeadWebhookDeliveries(ctx, limit)
}

func (s *service) GetSettings(ctx context.Context) (model.Settings, error) {
	_ = ctx
	return s.runtime.Settings(), nil
}

func (s *service) UpdateSettings(ctx context.Context, settings model.Settings) (model.Settings, error) {
	if err := config.SaveFileSettings(s.cfg.SettingsPath, config.FileSettings{
		ListenAddr: settings.ListenAddr,
		WebhookURL: settings.WebhookURL,
	}); err != nil {
		return model.Settings{}, err
	}
	if err := s.runtime.UpdateSettings(ctx, settings); err != nil {
		return model.Settings{}, err
	}
	return s.runtime.Settings(), nil
}

func (s *service) LogoutAccount(ctx context.Context, accountID string) error {
	s.pollers.StopAccount(accountID)
	if err := s.store.DeleteAccount(ctx, accountID); err != nil {
		return err
	}
	_ = s.store.AddLog(context.Background(), "INFO", "account disconnected locally", "account", fmt.Sprintf(`{"account_id":%q}`, accountID))
	return nil
}

func (s *service) RetryDeadLetter(ctx context.Context, id int64) error {
	if err := s.store.RetryDeadWebhookDelivery(ctx, id); err != nil {
		return err
	}
	if s.webhooks != nil {
		s.webhooks.Wakeup()
	}
	_ = s.store.AddLog(context.Background(), "INFO", "dead webhook delivery requeued", "webhook", fmt.Sprintf(`{"delivery_id":%d}`, id))
	return nil
}

func (s *service) SendText(ctx context.Context, accountID, toUserID, text, contextToken string) error {
	account, err := s.store.GetAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if contextToken == "" {
		if peerCtx, err := s.store.GetPeerContext(ctx, accountID, toUserID); err == nil {
			contextToken = peerCtx.ContextToken
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if strings.TrimSpace(contextToken) == "" {
		return errors.New("context token not found for this user; current text sending only supports replying to users who have already sent a message")
	}
	if err := s.client.SendTextMessage(ctx, account.BaseURL, account.Token, toUserID, text, contextToken); err != nil {
		_ = s.store.AddLog(context.Background(), "ERROR", "outbound send failed", "message", fmt.Sprintf(`{"account_id":%q,"to_user_id":%q,"err":%q}`, accountID, toUserID, err.Error()))
		return err
	}
	raw := fmt.Sprintf(`{"to_user_id":%q,"text":%q,"context_token":%q}`, toUserID, text, contextToken)
	if err := s.store.CreateOutboundEvent(ctx, accountID, toUserID, contextToken, text, raw); err != nil {
		return err
	}
	_ = s.store.AddLog(context.Background(), "INFO", "outbound text sent", "message", raw)
	return nil
}

func (s *service) HandleInboundMessage(ctx context.Context, account model.Account, msg ilink.WeixinMessage) error {
	event, inserted, err := s.store.SaveInboundMessage(ctx, account.AccountID, msg)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}

	payload, err := json.Marshal(map[string]any{
		"event_id":       event.ID,
		"event_type":     event.EventType,
		"account_id":    account.AccountID,
		"base_url":      account.BaseURL,
		"from_user_id":  msg.FromUserID,
		"to_user_id":    msg.ToUserID,
		"message_id":    msg.MessageID,
		"context_token": msg.ContextToken,
		"raw_message":   msg,
		"received_at":   time.Now().UTC(),
	})
	if err != nil {
		return err
	}

	settings := s.runtime.Settings()
	if settings.WebhookURL == "" {
		text := "[non-text]"
		for _, item := range msg.ItemList {
			if item.Type == 1 && item.TextItem != nil && item.TextItem.Text != "" {
				text = item.TextItem.Text
				break
			}
			if item.Type == 3 && item.VoiceItem != nil && item.VoiceItem.Text != "" {
				text = item.VoiceItem.Text
				break
			}
		}
		return s.store.AddLog(ctx, "INFO", fmt.Sprintf("inbound message from %s: %s", msg.FromUserID, text), "inbound", string(payload))
	}

	if err := s.store.EnqueueWebhookDelivery(ctx, event, payload); err != nil {
		return err
	}
	if s.webhooks != nil {
		s.webhooks.Wakeup()
	}
	return s.store.AddLog(ctx, "INFO", "inbound message queued for webhook", "webhook", string(payload))
}
