package main

import (
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"strings"

	"go.temporal.io/server/common/authorization"
	"go.temporal.io/server/common/config"
	"go.temporal.io/server/common/log"
)

// dualClaimMapper resolves caller identity from one of two credential sources and
// maps it to Temporal roles. It is the one piece of authorization logic we own;
// the Authorizer stays Temporal's default (see infra/temporal/AUTHZ.md §2).
//
//   - Service workers present an mTLS client certificate (no token). The cert's
//     identity maps to exactly one namespace with Worker|Writer.
//   - Humans (UI / CLI) present an Entra bearer JWT. That path delegates to
//     Temporal's default JWT claim mapper, which validates the signature and
//     reads namespace roles from the token's `roles` claim (§6).
//
// Phase 1 exercises only the certificate branch; the JWT delegate is nil unless
// the server config declares a JWT key provider, which happens in Phase 3.
type dualClaimMapper struct {
	jwt authorization.ClaimMapper // human path; nil until JWT is configured
}

// newDualClaimMapper is the factory handed to temporal.WithClaimMapper. It builds
// the JWT delegate only when the config actually declares JWT key sources, so in
// Phase 1/2 (no authorization block) it returns a mapper with the human path off.
func newDualClaimMapper(cfg *config.Config, logger log.Logger) authorization.ClaimMapper {
	var jwt authorization.ClaimMapper
	if cfg != nil && len(cfg.Global.Authorization.JWTKeyProvider.KeySourceURIs) > 0 {
		provider := authorization.NewDefaultTokenKeyProvider(&cfg.Global.Authorization, logger)
		jwt = authorization.NewDefaultJWTClaimMapper(provider, &cfg.Global.Authorization, logger)
	}
	return &dualClaimMapper{jwt: jwt}
}

func (m *dualClaimMapper) GetClaims(info *authorization.AuthInfo) (*authorization.Claims, error) {
	// Human path: a bearer token was presented. Delegate to the default JWT
	// mapper, which owns signature validation and namespace:role parsing.
	if info.AuthToken != "" {
		if m.jwt == nil {
			return nil, errors.New("bearer token presented but JWT authentication is not configured")
		}
		return m.jwt.GetClaims(info)
	}

	// Service path: an mTLS client certificate, no token.
	if info.TLSSubject != nil {
		ns, err := namespaceFromCert(info.TLSSubject)
		if err != nil {
			return nil, err
		}
		return &authorization.Claims{
			Subject: info.TLSSubject.CommonName,
			Namespaces: map[string]authorization.Role{
				ns: authorization.RoleWorker | authorization.RoleWriter,
			},
		}, nil
	}

	return nil, errors.New("no client certificate or bearer token presented")
}

// namespaceFromCert derives the single namespace a service certificate may access.
//
// Convention (AUTHZ.md §7): the cert CN is the namespace name, e.g. CN=svc-orders
// grants access to namespace "svc-orders". Only svc-* identities map to a
// namespace; anything else is rejected so a stray or human cert can't be treated
// as a service.
//
// TODO(phase-2): lock CN vs SAN URI (SPIFFE) as the identity source and align it
// with what `make issue-cert` writes into the certificate.
func namespaceFromCert(subject *pkix.Name) (string, error) {
	cn := strings.TrimSpace(subject.CommonName)
	if cn == "" {
		return "", errors.New("client certificate has no common name")
	}
	if !strings.HasPrefix(cn, "svc-") {
		return "", fmt.Errorf("certificate CN %q is not a service identity (expected svc-*)", cn)
	}
	return cn, nil
}
