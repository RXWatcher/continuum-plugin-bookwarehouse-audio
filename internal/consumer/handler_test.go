package consumer_test

import (
	"context"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/consumer"
)

func mustStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("structpb: %v", err)
	}
	return s
}

// Capability servers serve before Configure runs. There is NO reconciler or
// scheduled task in this plugin, so an event acked-and-dropped here is lost
// forever (the old code's "the portal will retry via reconciler" comment was
// false). It must nack so the host redelivers once configured.
func TestConsumer_NotConfigured_Nacks(t *testing.T) {
	h := consumer.New(func() *consumer.Deps { return nil }, nil)
	resp, err := h.HandleEvent(context.Background(), &pluginv1.HandleEventRequest{
		EventName: "plugin.continuum.audiobooks.request_submitted",
		Payload: mustStruct(t, map[string]any{
			"request_id":       "r-cfg",
			"target_plugin_id": consumer.PluginID,
			"title":            "X",
		}),
	})
	if err == nil {
		t.Fatal("not-configured must return an error so the host redelivers")
	}
	if resp != nil {
		t.Errorf("response must be nil on nack; got %+v", resp)
	}
}

// A foreign / wrong-event message is not ours: ack (no error) and drop so the
// host does not redeliver another plugin's event to us forever.
func TestConsumer_NonTargetEvent_Acks(t *testing.T) {
	h := consumer.New(func() *consumer.Deps { return nil }, nil)
	if _, err := h.HandleEvent(context.Background(), &pluginv1.HandleEventRequest{
		EventName: "some.other.event",
	}); err != nil {
		t.Fatalf("foreign event must be acked, not nacked; got err=%v", err)
	}
}
