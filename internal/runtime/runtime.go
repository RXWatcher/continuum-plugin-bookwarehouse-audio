// Package runtime implements the plugin's Runtime gRPC server. It embeds
// runtimedefault.Server (which handles BindHostBroker) and adds GetManifest
// and Configure handlers. Configure invokes a callback supplied by main.go
// so the plugin can (re)wire its pool/store/clients.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimedefault"
)

// Config is the parsed plugin global config.
type Config struct {
	BaseURL          string `json:"base_url"`
	APIKey           string `json:"api_key"`
	DefaultCoverSize string `json:"default_cover_size"`
	DirectFileAccess bool   `json:"direct_file_access"`
	PathRemappings   []PathRemapping
}

// PathRemapping converts paths returned by Book Warehouse into local mount
// paths available to this plugin process.
type PathRemapping struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path"`
}

// Configured returns true when all required fields are populated.
func (c Config) Configured() bool {
	return c.BaseURL != "" && c.APIKey != ""
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
		case "base_url":
			cfg.BaseURL = stringFromValue(m["value"], firstString(m))
		case "api_key":
			cfg.APIKey = stringFromValue(m["value"], firstString(m))
		case "default_cover_size":
			cfg.DefaultCoverSize = stringFromValue(m["value"], firstString(m))
		case "direct_file_access":
			cfg.DirectFileAccess = boolFromValue(m["value"])
		case "path_remappings":
			remaps, err := pathRemappingsFromValue(m["value"])
			if err != nil {
				return nil, fmt.Errorf("path_remappings: %w", err)
			}
			cfg.PathRemappings = remaps
		}
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("api_key is required")
	}
	if err := validateBaseURL(cfg.BaseURL); err != nil {
		return nil, fmt.Errorf("base_url: %w", err)
	}
	if !cfg.DirectFileAccess && len(cfg.PathRemappings) > 0 {
		return nil, fmt.Errorf("path_remappings require direct_file_access")
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

func boolFromValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func pathRemappingsFromValue(v any) ([]PathRemapping, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, nil
	}
	out := make([]PathRemapping, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("[%d] must be an object", i)
		}
		source := stringFromValue(m["source_path"], m["sourcePath"])
		target := stringFromValue(m["target_path"], m["targetPath"])
		if source == "" || target == "" {
			return nil, fmt.Errorf("[%d] source_path and target_path are required", i)
		}
		if !filepath.IsAbs(source) {
			return nil, fmt.Errorf("[%d] source_path must be absolute", i)
		}
		if !filepath.IsAbs(target) {
			return nil, fmt.Errorf("[%d] target_path must be absolute", i)
		}
		out = append(out, PathRemapping{SourcePath: source, TargetPath: target})
	}
	return out, nil
}

func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("scheme must be http or https")
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("must be an origin URL without credentials, query, or fragment")
	}
	if u.Scheme == "http" && !isLocalhost(u.Hostname()) {
		return fmt.Errorf("must use https except for localhost")
	}
	return nil
}

func isLocalhost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// MarshalConfig is exported for tests that need to verify the config decode.
func (s *Server) MarshalConfig() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return json.Marshal(s.cfg)
}
