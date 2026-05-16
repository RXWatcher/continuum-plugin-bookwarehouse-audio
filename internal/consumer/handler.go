// Package consumer implements event_consumer.v1 — receives
// plugin.continuum.audiobooks.request_submitted events, forwards them to
// BookWarehouse monitoring, persists state, and emits status events.
package consumer

import (
	"context"
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
	d := h.depsFn()
	if d == nil || d.Store == nil || d.Client == nil {
		// Plugin not fully configured; ignore — the portal will retry via
		// reconciler.
		return &pluginv1.HandleEventResponse{}, nil
	}
	if req.GetEventName() != "plugin.continuum.audiobooks.request_submitted" {
		return &pluginv1.HandleEventResponse{}, nil
	}
	if req.GetPayload() == nil {
		return &pluginv1.HandleEventResponse{}, nil
	}
	p := req.GetPayload().AsMap()

	// Filter on target_plugin_id.
	target := targetPluginIDFromPayload(p)
	if target != "" && target != PluginID {
		return &pluginv1.HandleEventResponse{}, nil
	}

	requestID := requestIDFromPayload(p)
	if requestID == "" {
		h.logger.Warn("request_submitted missing request_id")
		return &pluginv1.HandleEventResponse{}, nil
	}
	title, _ := p["title"].(string)
	author, _ := p["author"].(string)
	isbn, _ := p["isbn"].(string)
	if title == "" {
		h.logger.Warn("request_submitted missing title", "request_id", requestID)
		return &pluginv1.HandleEventResponse{}, nil
	}

	// Persist initial state.
	now := time.Now()
	if err := d.Store.UpsertForwardedRequest(ctx, store.ForwardedRequest{
		RequestID: requestID,
		Status:    "submitted",
		UpdatedAt: now,
	}); err != nil {
		h.logger.Warn("upsert forwarded_request", "err", err)
		return &pluginv1.HandleEventResponse{}, nil
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
			h.logger.Warn("upsert forwarded_request (after upstream err)",
				"request_id", requestID, "upstream_err", err, "db_err", uerr)
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
		// Logged but not fatal: the upstream already accepted the request, so
		// reporting failure to the host would cause the event to retry and
		// duplicate-add upstream. The reconciler will heal the DB row on
		// the next tick.
		h.logger.Warn("upsert forwarded_request (after acknowledged)",
			"request_id", requestID, "external_id", resp.ID, "db_err", uerr)
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
