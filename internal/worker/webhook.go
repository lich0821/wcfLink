package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"wcfLink/internal/model"
	"wcfLink/internal/store"
)

type WebhookDispatcher struct {
	store    *store.Store
	logger   *slog.Logger
	settings func() model.Settings
	client   *http.Client
	trigger  chan struct{}
	cancel   context.CancelFunc
}

func NewWebhookDispatcher(st *store.Store, settings func() model.Settings, logger *slog.Logger) *WebhookDispatcher {
	return &WebhookDispatcher{
		store:    st,
		logger:   logger,
		settings: settings,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		trigger: make(chan struct{}, 1),
	}
}

func (d *WebhookDispatcher) Start(parent context.Context) {
	if d == nil || d.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	d.cancel = cancel
	go d.run(ctx)
	d.Wakeup()
}

func (d *WebhookDispatcher) Stop() {
	if d == nil || d.cancel == nil {
		return
	}
	d.cancel()
	d.cancel = nil
}

func (d *WebhookDispatcher) Wakeup() {
	if d == nil {
		return
	}
	select {
	case d.trigger <- struct{}{}:
	default:
	}
}

func (d *WebhookDispatcher) run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		if !d.processBatch(ctx) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-d.trigger:
		}
	}
}

func (d *WebhookDispatcher) processBatch(ctx context.Context) bool {
	settings := d.settings()
	if settings.WebhookURL == "" {
		return true
	}

	jobs, err := d.store.ListDueWebhookDeliveries(ctx, 20)
	if err != nil {
		d.logger.Error("list webhook deliveries failed", "err", err)
		_ = d.store.AddLog(context.Background(), "ERROR", "list webhook deliveries failed", "webhook", err.Error())
		return true
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return false
		}
		d.deliver(ctx, settings.WebhookURL, job)
	}
	return true
}

func (d *WebhookDispatcher) deliver(ctx context.Context, webhookURL string, job model.WebhookDelivery) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader([]byte(job.PayloadJSON)))
	if err != nil {
		d.fail(job, webhookURL, 0, fmt.Sprintf("build request: %v", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WcfLink-Delivery-ID", fmt.Sprintf("%d", job.ID))
	req.Header.Set("X-WcfLink-Event-ID", fmt.Sprintf("%d", job.EventID))
	req.Header.Set("X-WcfLink-Attempt", fmt.Sprintf("%d", job.AttemptCount+1))

	resp, err := d.client.Do(req)
	if err != nil {
		d.fail(job, webhookURL, 0, err.Error())
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		d.fail(job, webhookURL, resp.StatusCode, fmt.Sprintf("unexpected status %d", resp.StatusCode))
		return
	}

	if err := d.store.MarkWebhookDeliveryDelivered(context.Background(), job.ID, webhookURL, resp.StatusCode); err != nil {
		d.logger.Error("mark webhook delivered failed", "delivery_id", job.ID, "err", err)
		return
	}
	_ = d.store.AddLog(context.Background(), "INFO", "webhook delivered", "webhook", fmt.Sprintf(`{"delivery_id":%d,"event_id":%d,"status_code":%d}`, job.ID, job.EventID, resp.StatusCode))
}

func (d *WebhookDispatcher) fail(job model.WebhookDelivery, webhookURL string, statusCode int, errText string) {
	nextAttempt := job.AttemptCount + 1
	if nextAttempt >= job.MaxAttempts {
		if err := d.store.MarkWebhookDeliveryDead(context.Background(), job.ID, webhookURL, statusCode, errText); err != nil {
			d.logger.Error("mark webhook dead failed", "delivery_id", job.ID, "err", err)
			return
		}
		_ = d.store.AddLog(context.Background(), "ERROR", "webhook delivery moved to dead letter", "webhook", fmt.Sprintf(`{"delivery_id":%d,"event_id":%d,"status_code":%d,"err":%q}`, job.ID, job.EventID, statusCode, errText))
		return
	}

	nextAttemptAt := time.Now().UTC().Add(webhookRetryDelay(nextAttempt))
	if err := d.store.MarkWebhookDeliveryForRetry(context.Background(), job.ID, webhookURL, statusCode, errText, nextAttemptAt); err != nil {
		d.logger.Error("reschedule webhook delivery failed", "delivery_id", job.ID, "err", err)
		return
	}
	_ = d.store.AddLog(context.Background(), "WARN", "webhook delivery scheduled for retry", "webhook", fmt.Sprintf(`{"delivery_id":%d,"event_id":%d,"status_code":%d,"attempt":%d,"next_attempt_at":%q,"err":%q}`, job.ID, job.EventID, statusCode, nextAttempt, nextAttemptAt.Format(time.RFC3339Nano), errText))
}

func webhookRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 5 * time.Second
	case 2:
		return 15 * time.Second
	case 3:
		return 45 * time.Second
	case 4:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
}
