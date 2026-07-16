package main

import (
	"crypto/x509/pkix"
	"testing"

	"go.temporal.io/server/common/authorization"
)

// These tests cover the certificate branch and the guard rails. They need no
// running server, no database, and no network — the whole point of keeping the
// owned logic in a plain function is that it's unit-testable in isolation.
func TestDualClaimMapper_GetClaims(t *testing.T) {
	// No JWT delegate: the human path is expected to error, which is correct for
	// Phase 1/2 where JWT auth is not configured.
	m := &dualClaimMapper{}

	tests := []struct {
		name     string
		info     *authorization.AuthInfo
		wantNS   string
		wantRole authorization.Role
		wantErr  bool
	}{
		{
			name:     "valid service cert grants worker+writer on its namespace",
			info:     &authorization.AuthInfo{TLSSubject: &pkix.Name{CommonName: "svc-orders"}},
			wantNS:   "svc-orders",
			wantRole: authorization.RoleWorker | authorization.RoleWriter,
		},
		{
			name:    "non-service CN is rejected",
			info:    &authorization.AuthInfo{TLSSubject: &pkix.Name{CommonName: "alice"}},
			wantErr: true,
		},
		{
			name:    "empty CN is rejected",
			info:    &authorization.AuthInfo{TLSSubject: &pkix.Name{}},
			wantErr: true,
		},
		{
			name:    "no credential is rejected",
			info:    &authorization.AuthInfo{},
			wantErr: true,
		},
		{
			name:    "bearer token without JWT configured is rejected",
			info:    &authorization.AuthInfo{AuthToken: "Bearer abc.def.ghi"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := m.GetClaims(tt.info)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got claims %+v", claims)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := claims.Namespaces[tt.wantNS]; got != tt.wantRole {
				t.Fatalf("namespace %q role = %d, want %d", tt.wantNS, got, tt.wantRole)
			}
			// A service cert must never grant a system (cross-namespace) role.
			if claims.System != authorization.RoleUndefined {
				t.Fatalf("service cert leaked a system role: %d", claims.System)
			}
		})
	}
}
