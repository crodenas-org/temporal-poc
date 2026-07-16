package main

import (
	"log"
	"os"

	"go.temporal.io/server/common/authorization"
	"go.temporal.io/server/common/config"
	commonlog "go.temporal.io/server/common/log"
	"go.temporal.io/server/temporal"
)

// This binary is a thin wrapper around the standard Temporal server. Its only
// addition is compiling in our dualClaimMapper via temporal.WithClaimMapper.
// Everything else — services, config, storage — is stock Temporal.
//
// Bootstrap follows the official sample:
//   https://github.com/temporalio/samples-server/blob/main/extensibility/authorizer/server/main.go
//
// The config-load and logger lines are the version-sensitive surface: names here
// track go.temporal.io/server, so reconcile with `go build` against the pinned
// version. The dualClaimMapper itself is stable, owned code.
func main() {
	// config.Load(env, configDir, zone, &cfg) reads <configDir>/<env>.yaml
	// (merging optional base.yaml), so the config lives at
	// /etc/temporal/config/docker.yaml with env="docker".
	configDir := envOr("TEMPORAL_CONFIG_DIR", "/etc/temporal/config")
	env := envOr("TEMPORAL_ENVIRONMENT", "docker")
	zone := os.Getenv("TEMPORAL_AVAILABILITY_ZONE")

	var cfg config.Config
	if err := config.Load(env, configDir, zone, &cfg); err != nil {
		log.Fatalf("load config (env=%s dir=%s): %v", env, configDir, err)
	}

	logger := commonlog.NewZapLogger(commonlog.BuildZapLogger(cfg.Log))

	s, err := temporal.NewServer(
		temporal.ForServices(temporal.DefaultServices),
		temporal.WithConfig(&cfg),
		temporal.InterruptOn(temporal.InterruptCh()),
		// Custom claim mapper is compiled in but only invoked when the config's
		// global.authorization block enables a claimMapper. Phase 1 ships with
		// authorization disabled, so this is inert until Phase 2/3 turn it on —
		// which means the custom image is behavior-identical to auto-setup today.
		temporal.WithClaimMapper(func(cfg *config.Config) authorization.ClaimMapper {
			return newDualClaimMapper(cfg, logger)
		}),
		// Authorizer intentionally left as Temporal's default.
	)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	if err := s.Start(); err != nil {
		log.Fatalf("start server: %v", err)
	}
	log.Println("temporal-server stopped")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
