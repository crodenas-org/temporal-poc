package main

import (
	"log"
	"os"
	"time"

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
//
//	https://github.com/temporalio/samples-server/blob/main/extensibility/authorizer/server/main.go
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

	// Env-driven authorization toggle (keeps tenant/JWKS out of the committed
	// config; auth stays OFF unless TEMPORAL_AUTH_JWKS_URI is set). When set:
	// humans present Entra JWTs (default JWT mapper reads the `roles` claim) and
	// our dualClaimMapper's JWT delegate activates because KeySourceURIs becomes
	// non-empty. Services still use the mTLS cert path. See AUTHZ.md §6.
	authEnabled := os.Getenv("TEMPORAL_AUTH_JWKS_URI") != ""
	if authEnabled {
		a := &cfg.Global.Authorization
		a.JWTKeyProvider.KeySourceURIs = []string{os.Getenv("TEMPORAL_AUTH_JWKS_URI")}
		a.JWTKeyProvider.RefreshInterval = time.Minute
		a.PermissionsClaimName = "roles"
		logger.Info("authorization ENABLED via TEMPORAL_AUTH_JWKS_URI")
	}

	// Run internal-frontend alongside the default services so Temporal's own
	// system workers have a non-authorized path to the frontend. Without it,
	// enabling the authorizer rejects internal workers ("Request unauthorized")
	// and the server exits. Paired with publicClient being omitted in docker.yaml.
	svcs := temporal.DefaultServices
	svcs = append(svcs[:len(svcs):len(svcs)], "internal-frontend")

	opts := []temporal.ServerOption{
		temporal.ForServices(svcs),
		temporal.WithConfig(&cfg),
		temporal.InterruptOn(temporal.InterruptCh()),
		// Compiled-in dual mapper. With no authorizer (auth off) its result is
		// ignored, so plaintext dev keeps working (proven in Phase 1).
		temporal.WithClaimMapper(func(cfg *config.Config) authorization.ClaimMapper {
			return newDualClaimMapper(cfg, logger)
		}),
	}
	if authEnabled {
		// The library NewServer path does NOT build the authorizer from config —
		// without an explicit authorizer it stays noop (allow-all) even with the
		// JWT config above. Inject one so claims are actually enforced. Omitted
		// when auth is off, so plaintext dev allows everything.
		//
		// The default authorizer does all the real work; our wrapper only widens
		// the handful of cluster-scoped APIs the OSS UI needs to render for a
		// namespace-scoped human (see authorizer.go / AUTHZ.md §15).
		opts = append(opts, temporal.WithAuthorizer(
			newUIRenderAuthorizer(authorization.NewDefaultAuthorizer()),
		))
		// ...and because that allows ListNamespaces, trim its response to the
		// caller's own namespaces. Without this the switcher advertises namespaces
		// the user can't open, and the stock UI turns the resulting 403 into a
		// login loop (interceptor.go / AUTHZ.md §14 #14).
		opts = append(opts, temporal.WithChainedFrontendGrpcInterceptors(
			newNamespaceFilterInterceptor(),
		))
	}

	s, err := temporal.NewServer(opts...)
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
