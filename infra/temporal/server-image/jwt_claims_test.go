package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"go.temporal.io/server/common/api"
	"go.temporal.io/server/common/authorization"
	"go.temporal.io/server/common/config"
	commonlog "go.temporal.io/server/common/log"
)

// These tests pin the JWT (human) path end to end: an Entra-shaped token in,
// Temporal Claims out, then the authorizer's decision on a real API name. They
// exist because of a bug that cost two sessions:
//
// A role value of "system:admin" reads like cluster admin but grants NOTHING at
// cluster scope. The default JWT claim mapper only sets claims.System when the
// namespace part equals primitives.SystemLocalNamespace — the literal string
// "temporal-system". "system:admin" silently becomes a namespace role on a
// namespace named "system", which does not exist, so claims.System stays 0 and
// every ScopeCluster API (ListNamespaces, GetSystemInfo, GetClusterInfo — the
// OSS UI's landing page) is denied.
//
// It is invisible in a positive test: an admin who can't list namespaces looks
// identical to a UI that's merely broken. TestSystemPrefix_Regression is the
// guard — if it fails, the app-role values in scripts/entra-app-setup.sh and the
// cluster-scope prefix have drifted apart.

const listNamespacesAPI = "/temporal.api.workflowservice.v1.WorkflowService/ListNamespaces"

// testKeyProvider serves one locally generated RSA public key, so the JWT path
// can be exercised with no network and no real JWKS endpoint.
type testKeyProvider struct{ pub *rsa.PublicKey }

func (p *testKeyProvider) RsaKey(string, string) (*rsa.PublicKey, error) { return p.pub, nil }
func (p *testKeyProvider) EcdsaKey(string, string) (*ecdsa.PublicKey, error) {
	return nil, errors.New("unsupported")
}
func (p *testKeyProvider) HmacKey(string, string) ([]byte, error) {
	return nil, errors.New("unsupported")
}
func (p *testKeyProvider) SupportedMethods() []string { return []string{"RS256"} }
func (p *testKeyProvider) Close()                     {}

// newJWTMapper wires our dualClaimMapper to the real default JWT delegate,
// substituting only the key source. permissionsClaimName mirrors main.go.
func newJWTMapper(t *testing.T) (*dualClaimMapper, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cfg := &config.Authorization{PermissionsClaimName: "roles"}
	logger := commonlog.NewNoopLogger()
	delegate := authorization.NewDefaultJWTClaimMapper(&testKeyProvider{pub: &key.PublicKey}, cfg, logger)
	return &dualClaimMapper{jwt: delegate}, key
}

// mintToken builds a signed Entra-shaped token carrying the given app roles.
func mintToken(t *testing.T, key *rsa.PrivateKey, roles ...string) string {
	t.Helper()
	r := make([]interface{}, len(roles))
	for i, role := range roles {
		r[i] = role
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":   "alice@example.com",
		"roles": r,
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "test-key"
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return "Bearer " + signed
}

// TestHumanToken_PrefersIDTokenFromExtras pins the header routing. The UI sends a
// role-less Microsoft Graph access token in Authorization and the role-bearing ID
// token in Authorization-Extras; reading the wrong one fails every request with
// "crypto/rsa: verification error" and denies everything. See AUTHZ.md §14.
func TestHumanToken_PrefersIDTokenFromExtras(t *testing.T) {
	mapper, key := newJWTMapper(t)
	idToken := mintToken(t, key, "temporal-system:admin")

	tests := []struct {
		name string
		info *authorization.AuthInfo
	}{
		{
			name: "extras wins over an unverifiable Authorization header",
			info: &authorization.AuthInfo{
				AuthToken: "Bearer not.a.validtoken", // stands in for the Graph token
				ExtraData: idToken,
			},
		},
		{
			name: "extras arriving without the Bearer scheme is normalized",
			info: &authorization.AuthInfo{
				AuthToken: "Bearer not.a.validtoken",
				ExtraData: strings.TrimPrefix(idToken, "Bearer "),
			},
		},
		{
			name: "no extras falls back to Authorization (the CLI path)",
			info: &authorization.AuthInfo{AuthToken: idToken},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := mapper.GetClaims(tt.info)
			if err != nil {
				t.Fatalf("GetClaims: %v", err)
			}
			if claims.System != authorization.RoleAdmin {
				t.Errorf("System = %d, want RoleAdmin (%d) — the role-bearing token was not used",
					claims.System, authorization.RoleAdmin)
			}
		})
	}
}

// TestAudiencePin rejects a token minted for a different app in the same tenant.
// Without it, any tenant-signed ID token would authorize here.
func TestAudiencePin(t *testing.T) {
	mapper, key := newJWTMapper(t)
	mapper.audience = "our-client-id"

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":   "alice@example.com",
		"roles": []interface{}{"temporal-system:admin"},
		"aud":   "some-other-app",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "test-key"
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := mapper.GetClaims(&authorization.AuthInfo{ExtraData: signed}); err == nil {
		t.Fatal("token for a different audience was accepted")
	}
}

func TestJWTClaims_ScopeMapping(t *testing.T) {
	mapper, key := newJWTMapper(t)

	tests := []struct {
		name       string
		roles      []string
		wantSystem authorization.Role
		wantNS     map[string]authorization.Role
	}{
		{
			name:       "temporal-system:admin grants cluster admin",
			roles:      []string{"temporal-system:admin"},
			wantSystem: authorization.RoleAdmin,
		},
		{
			name:       "temporal-system:read grants cluster read",
			roles:      []string{"temporal-system:read"},
			wantSystem: authorization.RoleReader,
		},
		{
			name:       "default:read grants namespace read only",
			roles:      []string{"default:read"},
			wantSystem: authorization.RoleUndefined,
			wantNS:     map[string]authorization.Role{"default": authorization.RoleReader},
		},
		{
			name:       "multiple namespace roles accumulate",
			roles:      []string{"default:read", "svc-demo:read"},
			wantSystem: authorization.RoleUndefined,
			wantNS: map[string]authorization.Role{
				"default":  authorization.RoleReader,
				"svc-demo": authorization.RoleReader,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := mapper.GetClaims(&authorization.AuthInfo{
				AuthToken: mintToken(t, key, tt.roles...),
			})
			if err != nil {
				t.Fatalf("GetClaims: %v", err)
			}
			if claims.System != tt.wantSystem {
				t.Errorf("System = %d, want %d", claims.System, tt.wantSystem)
			}
			for ns, want := range tt.wantNS {
				if got := claims.Namespaces[ns]; got != want {
					t.Errorf("Namespaces[%q] = %d, want %d", ns, got, want)
				}
			}
		})
	}
}

// TestSystemPrefix_Regression pins the exact trap: "system:*" is not cluster
// scope. If someone "tidies" the app-role values back to system:admin, this
// fails instead of silently 403ing every UI login.
func TestSystemPrefix_Regression(t *testing.T) {
	mapper, key := newJWTMapper(t)

	claims, err := mapper.GetClaims(&authorization.AuthInfo{
		AuthToken: mintToken(t, key, "system:admin"),
	})
	if err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	if claims.System != authorization.RoleUndefined {
		t.Fatalf("system:admin unexpectedly granted cluster scope (System=%d) — "+
			"the mapper's system prefix may have changed; re-check "+
			"scripts/entra-app-setup.sh", claims.System)
	}
	if got := claims.Namespaces["system"]; got != authorization.RoleAdmin {
		t.Fatalf("expected system:admin to land as a namespace role on %q, got %d", "system", got)
	}
}

// TestAuthorizer_ListNamespaces ties claims to the decision the OSS UI's landing
// page actually depends on. ListNamespaces is ScopeCluster, so only a
// temporal-system role reaches it.
func TestAuthorizer_ListNamespaces(t *testing.T) {
	if md := api.GetMethodMetadata(listNamespacesAPI); md.Scope != api.ScopeCluster {
		t.Fatalf("precondition: ListNamespaces scope = %v, want ScopeCluster", md.Scope)
	}

	mapper, key := newJWTMapper(t)
	authorizer := authorization.NewDefaultAuthorizer()

	tests := []struct {
		role      string
		wantAllow bool
	}{
		{role: "temporal-system:admin", wantAllow: true},
		{role: "temporal-system:read", wantAllow: true},
		{role: "system:admin", wantAllow: false}, // the bug: reads like admin, grants nothing
		{role: "default:read", wantAllow: false}, // namespace scope can't list namespaces
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			claims, err := mapper.GetClaims(&authorization.AuthInfo{
				AuthToken: mintToken(t, key, tt.role),
			})
			if err != nil {
				t.Fatalf("GetClaims: %v", err)
			}
			result, err := authorizer.Authorize(context.Background(), claims, &authorization.CallTarget{
				APIName: listNamespacesAPI,
			})
			if err != nil {
				t.Fatalf("Authorize: %v", err)
			}
			allowed := result.Decision == authorization.DecisionAllow
			if allowed != tt.wantAllow {
				t.Errorf("ListNamespaces with role %q: allowed=%v, want %v", tt.role, allowed, tt.wantAllow)
			}
		})
	}
}
