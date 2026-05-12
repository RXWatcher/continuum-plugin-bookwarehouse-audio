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
	"sync/atomic"

	"github.com/hashicorp/go-hclog"
	"github.com/jackc/pgx/v5/pgxpool"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	publicmanifest "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtime"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/consumer"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/event"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/httproutes"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/migrate"
	pluginrt "github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/server"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/store"
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

	// State holders updated atomically when Configure runs.
	var (
		poolPtr   atomic.Pointer[pgxpool.Pool]
		storePtr  atomic.Pointer[store.Store]
		clientPtr atomic.Pointer[bookwarehouse.Client]
		cfgPtr    atomic.Pointer[pluginrt.Config]
		eventsPtr atomic.Pointer[event.Publisher]
	)

	rt := pluginrt.New(manifest, func(cfg pluginrt.Config) error {
		ctx := context.Background()

		p, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("pgxpool: %w", err)
		}
		if err := migrate.Run(ctx, cfg.DatabaseURL); err != nil {
			p.Close()
			return fmt.Errorf("migrate: %w", err)
		}

		s := store.New(p)
		bwClient := bookwarehouse.NewClient(cfg.BaseURL, cfg.APIKey)
		ev := event.New(sdkruntime.Host(), logger)

		srv := server.New(server.Deps{
			BookwarehouseClient: bwClient,
			Store:               s,
		})
		httpSrv.SetHandler(srv.Handler())

		storePtr.Store(s)
		clientPtr.Store(bwClient)
		eventsPtr.Store(ev)
		cfgPtr.Store(&cfg)

		if old := poolPtr.Swap(p); old != nil {
			old.Close()
		}
		logger.Info("configured", "base_url", cfg.BaseURL)
		return nil
	})

	cons := consumer.New(func() *consumer.Deps {
		s := storePtr.Load()
		c := clientPtr.Load()
		ev := eventsPtr.Load()
		cfg := cfgPtr.Load()
		if s == nil || c == nil {
			return nil
		}
		profile := ""
		if cfg != nil {
			profile = cfg.RequestQualityProfile
		}
		return &consumer.Deps{
			Store:          s,
			Client:         c,
			Events:         ev,
			QualityProfile: profile,
		}
	}, logger)

	sdkruntime.Serve(sdkruntime.ServeConfig{
		Logger: logger,
		Servers: sdkruntime.CapabilityServers{
			Runtime:       rt,
			HttpRoutes:    httpSrv,
			EventConsumer: cons,
		},
	})
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
