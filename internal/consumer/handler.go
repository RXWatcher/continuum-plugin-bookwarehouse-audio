// Package consumer implements event_consumer.v1 — receives
// plugin.continuum.audiobooks.request_submitted events, forwards them to
// BookWarehouse monitoring, persists state, and emits status events.
package consumer

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/go-hclog"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/store"
)

// PluginID is this plugin's id. Used to filter inbound request_submitted
// events on payload.target_plugin_id.
const PluginID = "continuum.bookwarehouse-audio"

// Eventer is the publish-side dependency. Defined as an interface so tests
// can substitute a fake.
type Eventer interface {
	Publish(ctx context.Context, name string, payload map[string]any)
}

// Deps is the runtime state the consumer needs. Resolved per-event so the
// store can be wired after Configure runs.
type Deps struct {
	Store          *store.Store
	Client         *bookwarehouse.Client
	Events         Eventer
	QualityProfile string
}

// Handler implements pluginv1.EventConsumerServer.
type Handler struct {
	pluginv1.UnimplementedEventConsumerServer
	depsFn func() *Deps
	logger hclog.Logger
}

// New constructs a Handler. depsFn returns nil before Configure has run.
func New(depsFn func() *Deps, logger hclog.Logger) *Handler {
	if logger == nil {
		logger = hclog.NewNullLogger()
	}
	return &Handler{depsFn: depsFn, logger: logger}
}

// HandleEvent dispatches one host event by name.
func (h *Handler) HandleEvent(ctx context.Context, req *pluginv1.HandleEventRequest) (*pluginv1.HandleEventResponse, error) {
	// Foreign / malformed events are not ours: ack (no error) and drop so the
	// host does not redeliver them forever. These checks need no deps.
	if req.GetEventName() != "plugin.continuum.audiobooks.request_submitted" {
		return &pluginv1.HandleEventResponse{}, nil
	}
	if req.GetPayload() == nil {
		return &pluginv1.HandleEventResponse{}, nil
	}
	p := req.GetPayload().AsMap()
	if target := targetPluginIDFromPayload(p); target != "" && target != PluginID {
		return &pluginv1.HandleEventResponse{}, nil
	}

	// This event IS ours. If the plugin is not configured yet, NACK so the
	// host redelivers once Configure has run. This plugin has no reconciler /
	// scheduled task, so acking-and-dropping here would lose the request
	// permanently (the old "the portal will retry via reconciler" comment was
	// false — no such reconciler exists).
	d := h.depsFn()
	if d == nil || d.Store == nil || d.Client == nil {
		return nil, fmt.Errorf("plugin not configured yet")
	}

	requestID := requestIDFromPayload(p)
	if requestID == "" {
		// Malformed payload — a permanent client error; ack (nacking would
		// poison-loop on the same bad event forever).
		h.logger.Warn("request_submitted missing request_id")
		return &pluginv1.HandleEventResponse{}, nil
	}
	title, _ := p["title"].(string)
	author, _ := p["author"].(string)
	isbn, _ := p["isbn"].(string)
	if title == "" {
		// Permanent client error; ack.
		h.logger.Warn("request_submitted missing title", "request_id", requestID)
		return &pluginv1.HandleEventResponse{}, nil
	}

	// Must persist before forwarding upstream: if this row is lost nothing
	// ever reconciles it (no reconciler) and the request is permanently lost.
	// Nack instead of starting untracked upstream work; the terminal guard in
	// UpsertForwardedRequest makes the inevitable redelivery idempotent.
	now := time.Now()
	if err := d.Store.UpsertForwardedRequest(ctx, store.ForwardedRequest{
		RequestID: requestID,
		Status:    "submitted",
		UpdatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("persist submitted %s: %w", requestID, err)
	}

	// Forward to upstream.
	resp, err := d.Client.AddMonitoring(ctx, bookwarehouse.MonitoringRequest{
		Title:          title,
		Author:         author,
		ISBN:           isbn,
		QualityProfile: d.QualityProfile,
	})
	if err != nil {
		if uerr := d.Store.UpsertForwardedRequest(ctx, store.ForwardedRequest{
			RequestID: requestID,
			Status:    "failed",
			ErrorText: err.Error(),
			UpdatedAt: time.Now(),
		}); uerr != nil {
			// Couldn't even record the failure — nack so it's retried rather
			// than left stuck non-terminal with no way to recover (no
			// reconciler exists in this plugin).
			return nil, fmt.Errorf("persist failed %s: %w (upstream: %v)", requestID, uerr, err)
		}
		if d.Events != nil {
			d.Events.Publish(ctx, "request_failed", map[string]any{
				"request_id":         requestID,
				"requestId":          requestID,
				"provider_plugin_id": PluginID,
				"reason":             err.Error(),
			})
		}
		return &pluginv1.HandleEventResponse{}, nil
	}

	if uerr := d.Store.UpsertForwardedRequest(ctx, store.ForwardedRequest{
		RequestID:  requestID,
		ExternalID: resp.ID,
		Status:     "acknowledged",
		UpdatedAt:  time.Now(),
	}); uerr != nil {
		// Must persist the external_id: this plugin has no reconciler, so a
		// row stuck at "submitted" with no external_id is never recovered and
		// the request hangs forever (the snapshot endpoint looks the row up
		// by external_id and would 404). Nack; the terminal guard makes
		// redelivery idempotent. Re-adding the same request upstream is the
		// accepted tradeoff vs. a permanently lost request.
		return nil, fmt.Errorf("persist acknowledged %s: %w", requestID, uerr)
	}
	if d.Events != nil {
		d.Events.Publish(ctx, "request_acknowledged", map[string]any{
			"request_id":         requestID,
			"requestId":          requestID,
			"external_id":        resp.ID,
			"provider_plugin_id": PluginID,
		})
	}
	return &pluginv1.HandleEventResponse{}, nil
}

func targetPluginIDFromPayload(p map[string]any) string {
	for _, key := range []string{"target_plugin_id", "target_provider_plugin_id", "provider_plugin_id"} {
		if v, _ := p[key].(string); v != "" {
			return v
		}
	}
	return ""
}

func requestIDFromPayload(p map[string]any) string {
	if id, _ := p["request_id"].(string); id != "" {
		return id
	}
	id, _ := p["requestId"].(string)
	return id
}
