// Package runtime implements the plugin's Runtime gRPC server. It embeds
// runtimedefault.Server (which handles BindHostBroker) and adds GetManifest
// and Configure handlers. Configure invokes a callback supplied by main.go
// so the plugin can (re)wire its pool/store/clients.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimedefault"
)

// Config is the parsed plugin global config.
type Config struct {
	DatabaseURL           string `json:"database_url"`
	BaseURL               string `json:"base_url"`
	APIKey                string `json:"api_key"`
	DefaultCoverSize      string `json:"default_cover_size"`
	RequestQualityProfile string `json:"request_quality_profile"`
}

// Configured returns true when all required fields are populated.
func (c Config) Configured() bool {
	return c.BaseURL != "" && c.APIKey != "" && c.DatabaseURL != ""
}

// Server implements the plugin's Runtime service.
type Server struct {
	runtimedefault.Server
	manifest *pluginv1.PluginManifest
	onCfg    func(Config) error

	mu  sync.RWMutex
	cfg Config
}

// New constructs a Runtime server bound to a manifest and a Configure callback.
func New(manifest *pluginv1.PluginManifest, onConfig func(Config) error) *Server {
	return &Server{manifest: manifest, onCfg: onConfig}
}

func (s *Server) GetManifest(_ context.Context, _ *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

func (s *Server) Configure(_ context.Context, req *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	cfg := Config{}
	for _, e := range req.GetConfig() {
		v := e.GetValue()
		if v == nil {
			continue
		}
		m := v.AsMap()
		switch e.GetKey() {
		case "database_url":
			cfg.DatabaseURL = stringFromValue(m["value"], firstString(m))
		case "base_url":
			cfg.BaseURL = stringFromValue(m["value"], firstString(m))
		case "api_key":
			cfg.APIKey = stringFromValue(m["value"], firstString(m))
		case "default_cover_size":
			cfg.DefaultCoverSize = stringFromValue(m["value"], firstString(m))
		case "request_quality_profile":
			cfg.RequestQualityProfile = stringFromValue(m["value"], firstString(m))
		}
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("database_url is required")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("api_key is required")
	}
	if s.onCfg != nil {
		if err := s.onCfg(cfg); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	return &pluginv1.ConfigureResponse{}, nil
}

// Snapshot returns a copy of the currently applied config.
func (s *Server) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func stringFromValue(candidates ...any) string {
	for _, c := range candidates {
		if s, ok := c.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func firstString(m map[string]any) any {
	for _, v := range m {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return nil
}

// MarshalConfig is exported for tests that need to verify the config decode.
func (s *Server) MarshalConfig() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return json.Marshal(s.cfg)
}
