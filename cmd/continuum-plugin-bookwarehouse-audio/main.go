// Command continuum-plugin-bookwarehouse-audio is the BookWarehouse-backed
// audiobook backend plugin entrypoint. See README.md and the design spec at
// docs/superpowers/specs/2026-05-11-audiobooks-portal-and-bookwarehouse-backend-design.md.
package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	goruntime "runtime"

	"github.com/hashicorp/go-hclog"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	publicmanifest "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtime"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/httproutes"
	pluginrt "github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/server"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/stream"
)

//go:embed manifest.json
var manifestRaw []byte

func main() {
	logger := hclog.New(&hclog.LoggerOptions{Name: "continuum-plugin-bookwarehouse-audio"})

	manifest, err := loadManifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load manifest: %v\n", err)
		os.Exit(1)
	}

	httpSrv := httproutes.NewServer()

	// This plugin is a pure audiobook library/catalog/stream source: no
	// state, no DB, no request forwarding. Configure just (re)builds the
	// upstream client and HTTP handler.
	rt := pluginrt.New(manifest, func(cfg pluginrt.Config) error {
		bwClient := bookwarehouse.NewClient(cfg.BaseURL, cfg.APIKey)
		srv := server.New(server.Deps{
			BookwarehouseClient: bwClient,
			StreamConfig: stream.Config{
				DirectFileAccess: cfg.DirectFileAccess,
				PathRemappings:   toStreamRemappings(cfg.PathRemappings),
			},
		})
		httpSrv.SetHandler(srv.Handler())
		logger.Info("configured", "base_url", cfg.BaseURL)
		return nil
	})

	sdkruntime.Serve(sdkruntime.ServeConfig{
		Logger: logger,
		Servers: sdkruntime.CapabilityServers{
			Runtime:    rt,
			HttpRoutes: httpSrv,
		},
	})
}

func toStreamRemappings(in []pluginrt.PathRemapping) []stream.PathRemapping {
	out := make([]stream.PathRemapping, 0, len(in))
	for _, item := range in {
		out = append(out, stream.PathRemapping{
			SourcePath: item.SourcePath,
			TargetPath: item.TargetPath,
		})
	}
	return out
}

func loadManifest() (*pluginv1.PluginManifest, error) {
	manifest, err := publicmanifest.Load(manifestRaw)
	if err != nil {
		return nil, fmt.Errorf("load embedded manifest: %w", err)
	}
	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	binaryData, err := os.ReadFile(executablePath)
	if err != nil {
		return nil, fmt.Errorf("read executable %q: %w", executablePath, err)
	}
	checksum := sha256.Sum256(binaryData)
	manifest.Checksum = hex.EncodeToString(checksum[:])
	if len(manifest.GetSupportedPlatforms()) == 0 {
		manifest.SupportedPlatforms = []*pluginv1.SupportedPlatform{
			{Os: goruntime.GOOS, Arch: goruntime.GOARCH},
		}
	}
	return manifest, nil
}
