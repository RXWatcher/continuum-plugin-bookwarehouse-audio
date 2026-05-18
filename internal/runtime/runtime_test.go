package runtime

import (
	"context"
	"testing"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func req(t *testing.T, kv map[string]any) *pluginv1.ConfigureRequest {
	t.Helper()
	entries := make([]*pluginv1.ConfigEntry, 0, len(kv))
	for k, v := range kv {
		s, err := structpb.NewStruct(map[string]any{"value": v})
		if err != nil {
			t.Fatalf("structpb: %v", err)
		}
		entries = append(entries, &pluginv1.ConfigEntry{Key: k, Value: s})
	}
	return &pluginv1.ConfigureRequest{Config: entries}
}

func TestConfigure_RejectsInsecureRemoteBaseURL(t *testing.T) {
	s := New(nil, func(Config) error { return nil })
	_, err := s.Configure(context.Background(), req(t, map[string]any{
		"base_url": "http://bookwarehouse.example",
		"api_key":  "k",
	}))
	if err == nil {
		t.Fatal("expected base_url error")
	}
}

func TestConfigure_AllowsLocalHTTPBaseURL(t *testing.T) {
	var got Config
	s := New(nil, func(c Config) error {
		got = c
		return nil
	})
	_, err := s.Configure(context.Background(), req(t, map[string]any{
		"base_url": "http://localhost:8080",
		"api_key":  "k",
	}))
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if got.BaseURL != "http://localhost:8080" {
		t.Fatalf("BaseURL = %q", got.BaseURL)
	}
}

func TestConfigure_RejectsInvalidPathRemapping(t *testing.T) {
	s := New(nil, func(Config) error { return nil })
	_, err := s.Configure(context.Background(), req(t, map[string]any{
		"base_url":           "https://bookwarehouse.example",
		"api_key":            "k",
		"direct_file_access": true,
		"path_remappings": []any{
			map[string]any{"source_path": "relative", "target_path": "/mnt/audio"},
		},
	}))
	if err == nil {
		t.Fatal("expected path_remappings error")
	}
}

func TestConfigure_RejectsRemappingWhenDirectAccessDisabled(t *testing.T) {
	s := New(nil, func(Config) error { return nil })
	_, err := s.Configure(context.Background(), req(t, map[string]any{
		"base_url": "https://bookwarehouse.example",
		"api_key":  "k",
		"path_remappings": []any{
			map[string]any{"source_path": "/warehouse", "target_path": "/mnt/audio"},
		},
	}))
	if err == nil {
		t.Fatal("expected direct_file_access error")
	}
}
