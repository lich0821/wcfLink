package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"wcfLink/internal/ilink"
)

func TestSaveInboundMessageDeduplicates(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	msg := ilink.WeixinMessage{
		MessageID:    1001,
		FromUserID:   "alice@im.wechat",
		ToUserID:     "bot@im.bot",
		ContextToken: "ctx-1",
		ItemList: []ilink.MessageItem{
			{
				Type: 1,
				TextItem: &ilink.TextItem{
					Text: "hello",
				},
			},
		},
	}

	first, inserted, err := st.SaveInboundMessage(ctx, "bot@im.bot", msg)
	if err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	if !inserted {
		t.Fatalf("expected first insert to be new")
	}
	if first.ID == 0 {
		t.Fatalf("expected first event id to be set")
	}

	second, inserted, err := st.SaveInboundMessage(ctx, "bot@im.bot", msg)
	if err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	if inserted {
		t.Fatalf("expected second insert to be deduplicated")
	}
	if second.ID != first.ID {
		t.Fatalf("expected duplicate event id %d, got %d", first.ID, second.ID)
	}
}

func TestWebhookDeliveryLifecycle(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	msg := ilink.WeixinMessage{
		MessageID:    2002,
		FromUserID:   "bob@im.wechat",
		ToUserID:     "bot@im.bot",
		ContextToken: "ctx-2",
		ItemList: []ilink.MessageItem{
			{
				Type: 1,
				TextItem: &ilink.TextItem{
					Text: "need webhook",
				},
			},
		},
	}
	event, inserted, err := st.SaveInboundMessage(ctx, "bot@im.bot", msg)
	if err != nil {
		t.Fatalf("save inbound failed: %v", err)
	}
	if !inserted {
		t.Fatalf("expected inbound event to be inserted")
	}

	if err := st.EnqueueWebhookDelivery(ctx, event, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("enqueue webhook failed: %v", err)
	}
	due, err := st.ListDueWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("list due failed: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected 1 due delivery, got %d", len(due))
	}

	if err := st.MarkWebhookDeliveryDead(ctx, due[0].ID, "https://example.com/webhook", 500, "boom"); err != nil {
		t.Fatalf("mark dead failed: %v", err)
	}
	dead, err := st.ListDeadWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("list dead failed: %v", err)
	}
	if len(dead) != 1 {
		t.Fatalf("expected 1 dead delivery, got %d", len(dead))
	}
	if dead[0].AttemptCount != 1 {
		t.Fatalf("expected attempt count 1, got %d", dead[0].AttemptCount)
	}

	if err := st.RetryDeadWebhookDelivery(ctx, dead[0].ID); err != nil {
		t.Fatalf("retry dead delivery failed: %v", err)
	}
	requeued, err := st.ListDueWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("list requeued failed: %v", err)
	}
	if len(requeued) != 1 {
		t.Fatalf("expected 1 requeued delivery, got %d", len(requeued))
	}
	if requeued[0].AttemptCount != 0 {
		t.Fatalf("expected attempt count reset to 0, got %d", requeued[0].AttemptCount)
	}
	if requeued[0].DeadLetterAt != nil {
		t.Fatalf("expected dead letter timestamp to be cleared")
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "wcfLink-test.db")
	st, err := New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("create store failed: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st
}

func TestRetryDeadWebhookDeliveryNotFound(t *testing.T) {
	st := newTestStore(t)
	err := st.RetryDeadWebhookDelivery(context.Background(), 999)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMarkWebhookDeliveryForRetrySchedulesFuture(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	msg := ilink.WeixinMessage{
		MessageID: 3003,
		ItemList: []ilink.MessageItem{
			{
				Type: 1,
				TextItem: &ilink.TextItem{
					Text: "retry me",
				},
			},
		},
	}
	event, _, err := st.SaveInboundMessage(ctx, "bot@im.bot", msg)
	if err != nil {
		t.Fatalf("save inbound failed: %v", err)
	}
	if err := st.EnqueueWebhookDelivery(ctx, event, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("enqueue webhook failed: %v", err)
	}
	due, err := st.ListDueWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("list due failed: %v", err)
	}
	next := time.Now().UTC().Add(5 * time.Minute)
	if err := st.MarkWebhookDeliveryForRetry(ctx, due[0].ID, "https://example.com/webhook", 502, "temporary", next); err != nil {
		t.Fatalf("mark retry failed: %v", err)
	}
	due, err = st.ListDueWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("list due after retry failed: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("expected no due deliveries after scheduling future retry, got %d", len(due))
	}
}
