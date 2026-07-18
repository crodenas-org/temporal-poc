package main

import (
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"os"
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
//   - Humans (UI / CLI) present an Entra JWT. That path delegates to Temporal's
//     default JWT claim mapper, which validates the signature and reads namespace
//     roles from the token's `roles` claim (§6).
//
// # Which token carries the roles
//
// The UI sends TWO credentials, and only one of them is usable:
//
//	Authorization:        an ACCESS token, aud=00000003-0000-0000-c000-000000000000
//	                      (Microsoft Graph). No `roles` claim, and signed by a key
//	                      we cannot verify against our tenant JWKS.
//	Authorization-Extras: the ID token, aud=<our client id>, carrying `roles`.
//
// Temporal's default mapper only ever reads AuthToken (the Authorization header),
// so on its own it validates the Graph token and fails with
// "crypto/rsa: verification error" — every request, silently, denying everything.
// Surfacing the ID token from ExtraData is exactly what Temporal exposes that
// field for, and it is why this custom mapper exists. See AUTHZ.md §14.
//
// The ID token is still fully validated by the delegate (signature vs. the tenant
// JWKS) and we additionally pin its audience to our own client id, so a token
// minted for a different app in the same tenant cannot be replayed here.
type dualClaimMapper struct {
	jwt    authorization.ClaimMapper // human path; nil until JWT is configured
	logger log.Logger                // may be nil in tests; only used for TEMPORAL_AUTH_DEBUG
	// audience, when set, is the client id every human token must be addressed to.
	// Empty disables the check (the delegate skips audience validation).
	audience string
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
	return &dualClaimMapper{
		jwt:      jwt,
		logger:   logger,
		audience: os.Getenv("TEMPORAL_AUTH_CLIENT_ID"),
	}
}

func (m *dualClaimMapper) GetClaims(info *authorization.AuthInfo) (*authorization.Claims, error) {
	if authDebugEnabled() && m.logger != nil {
		logAuthInfo(info, m.logger)
		claims, err := m.getClaims(info)
		logClaims(claims, err, m.logger)
		return claims, err
	}
	return m.getClaims(info)
}

func (m *dualClaimMapper) getClaims(info *authorization.AuthInfo) (*authorization.Claims, error) {
	// Human path. Prefer the ID token from Authorization-Extras: it is the only
	// one carrying `roles`, and the Authorization header from the UI holds a Graph
	// access token that would fail verification outright (see the type comment).
	// The CLI presents its token in Authorization with no extras, so fall back.
	if token := humanToken(info); token != "" {
		if m.jwt == nil {
			return nil, errors.New("bearer token presented but JWT authentication is not configured")
		}
		return m.jwt.GetClaims(&authorization.AuthInfo{
			AuthToken:     token,
			TLSConnection: info.TLSConnection,
			Audience:      m.audience,
		})
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

// humanToken picks the credential to authorize a human by, normalized to the
// "Bearer <jwt>" form the default JWT mapper expects. ExtraData wins because the
// UI puts the role-bearing ID token there; it arrives bare, without the scheme.
// Returns "" when neither header is present (the service/mTLS path).
func humanToken(info *authorization.AuthInfo) string {
	if extra := strings.TrimSpace(info.ExtraData); extra != "" {
		if !strings.HasPrefix(strings.ToLower(extra), "bearer ") {
			extra = "Bearer " + extra
		}
		return extra
	}
	return strings.TrimSpace(info.AuthToken)
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
