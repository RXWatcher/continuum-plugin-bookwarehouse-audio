// Command continuum-plugin-bookwarehouse-audio is the BookWarehouse-backed
// audiobook backend plugin entrypoint. See README.md and the design spec at
// docs/superpowers/specs/2026-05-11-audiobooks-portal-and-bookwarehouse-backend-design.md.
package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	goruntime "runtime"
	"sync"
	"sync/atomic"

	"github.com/hashicorp/go-hclog"
	"github.com/jackc/pgx/v5/pgxpool"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	publicmanifest "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtime"

	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/covers"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/httproutes"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/localfs"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/migrate"
	pluginrt "github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/runtime"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/server"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/store"
	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/stream"
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
	httpSrv.SetHandler(server.New(server.Deps{}).Handler())

	var (
		poolPtr  atomic.Pointer[pgxpool.Pool]
		storePtr atomic.Pointer[store.Store]
		// rebuildMu serializes rebuild() so concurrent admin saves and host
		// Configure RPCs don't race to swap the http handler.
		rebuildMu sync.Mutex
	)

	// Forward declarations so rebuild can pass refresh into server.Deps and
	// refresh can call rebuild — the classic self-referential closure pair.
	var (
		rebuild func(ctx context.Context, st *store.Store, cfg pluginrt.Config) error
		refresh func(ctx context.Context) error
	)

	// rebuild constructs the byte-serving Deps from the supplied app config
	// and atomically swaps the http handler. Single code path that turns
	// config into a running server — called both on host Configure and on
	// admin config saves (via the Refresh callback wired into Deps).
	rebuild = func(ctx context.Context, st *store.Store, cfg pluginrt.Config) error {
		var bwClient *bookwarehouse.Client
		if cfg.BaseURL != "" && cfg.APIKey != "" {
			bwClient = bookwarehouse.NewClient(cfg.BaseURL, cfg.APIKey)
		}
		resolver := localfs.New(cfg.LibraryRoot, toLocalfsRemappings(cfg.PathRemappings))
		var coverSvc *covers.Service
		if bwClient != nil && resolver.Configured() {
			cs, err := covers.NewService(bwClient, resolver, cfg.CoverCacheDir)
			if err != nil {
				logger.Warn("cover service unavailable", "err", err)
			} else {
				coverSvc = cs
			}
		}
		srv := server.New(server.Deps{
			BookwarehouseClient: bwClient,
			StreamConfig: stream.Config{
				LibraryRoot:         cfg.LibraryRoot,
				PathRemappings:      toStreamRemappings(cfg.PathRemappings),
				StreamSigningSecret: cfg.StreamSigningSecret,
			},
			Covers:  coverSvc,
			Store:   st,
			Config:  cfg,
			Refresh: refresh,
		})
		httpSrv.SetHandler(srv.Handler())
		return nil
	}

	// refresh re-reads the persisted app config and rebuilds. Admin handlers
	// call this after a successful save so resolver / covers service pick up
	// new path_remappings / library_root without requiring a re-upload.
	refresh = func(ctx context.Context) error {
		rebuildMu.Lock()
		defer rebuildMu.Unlock()
		st := storePtr.Load()
		if st == nil {
			return fmt.Errorf("store not initialised")
		}
		cfg, err := st.GetAppConfig(ctx)
		if err != nil {
			return fmt.Errorf("read app config: %w", err)
		}
		return rebuild(ctx, st, cfg)
	}

	rt := pluginrt.New(manifest, func(cfg pluginrt.Config) error {
		ctx := context.Background()
		pcfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("parse db: %w", err)
		}
		if pcfg.MaxConns < 8 {
			pcfg.MaxConns = 8
		}
		p, err := pgxpool.NewWithConfig(ctx, pcfg)
		if err != nil {
			return fmt.Errorf("pgxpool: %w", err)
		}
		if err := migrate.Run(ctx, cfg.DatabaseURL); err != nil {
			p.Close()
			return fmt.Errorf("migrate: %w", err)
		}
		st := store.New(p)
		appCfg, err := st.ImportLegacyAppConfig(ctx, cfg)
		if err != nil {
			p.Close()
			return fmt.Errorf("import app config: %w", err)
		}
		appCfg.DatabaseURL = cfg.DatabaseURL
		cfg = appCfg

		rebuildMu.Lock()
		storePtr.Store(st)
		err = rebuild(ctx, st, cfg)
		rebuildMu.Unlock()
		if err != nil {
			p.Close()
			return err
		}
		if old := poolPtr.Swap(p); old != nil {
			old.Close()
		}
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

func toLocalfsRemappings(in []pluginrt.PathRemapping) []localfs.Remapping {
	out := make([]localfs.Remapping, 0, len(in))
	for _, item := range in {
		out = append(out, localfs.Remapping{
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
