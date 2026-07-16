package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"go.temporal.io/server/common/authorization"
	"go.temporal.io/server/common/log"
	"go.temporal.io/server/common/log/tag"
)

// Diagnostics for the human auth path, enabled with TEMPORAL_AUTH_DEBUG=1.
//
// Authorization failures here are silent by design: the authorizer returns
// PermissionDenied without logging, so a misrouted or role-less token looks
// exactly like a correctly-denied one. This prints which credential surface the
// frontend actually received and what each token claims, so header/claim
// mismatches are visible instead of inferred.
//
// It never logs token contents — only the audience, the roles claim, and
// lengths. Keep it that way: these are live bearer tokens.

func authDebugEnabled() bool { return os.Getenv("TEMPORAL_AUTH_DEBUG") == "1" }

// describeToken decodes a JWT payload WITHOUT verifying its signature. It is for
// logging only — never make an authorization decision from this.
func describeToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "<empty>"
	}
	// Tolerate both "Bearer <jwt>" and a bare "<jwt>".
	if parts := strings.SplitN(raw, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		raw = parts[1]
	}
	segments := strings.Split(raw, ".")
	if len(segments) != 3 {
		return fmt.Sprintf("<not a JWT: %d segments, len=%d>", len(segments), len(raw))
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return fmt.Sprintf("<undecodable payload: %v>", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return fmt.Sprintf("<unparseable payload: %v>", err)
	}
	return fmt.Sprintf("aud=%v iss=%v roles=%v scp=%v appid=%v",
		claims["aud"], claims["iss"], claims["roles"], claims["scp"], claims["appid"])
}

func logAuthInfo(info *authorization.AuthInfo, logger log.Logger) {
	if info == nil {
		logger.Info("AUTHDEBUG: nil AuthInfo")
		return
	}
	logger.Info("AUTHDEBUG: credential surfaces received",
		tag.NewStringTag("authorization_header", describeToken(info.AuthToken)),
		tag.NewStringTag("authorization_extras_header", describeToken(info.ExtraData)),
		tag.NewStringTag("audience", info.Audience),
		tag.NewBoolTag("has_tls_subject", info.TLSSubject != nil),
	)
}

func logClaims(claims *authorization.Claims, err error, logger log.Logger) {
	if err != nil {
		logger.Info("AUTHDEBUG: claim mapping FAILED", tag.Error(err))
		return
	}
	if claims == nil {
		logger.Info("AUTHDEBUG: claim mapping returned nil claims")
		return
	}
	logger.Info("AUTHDEBUG: mapped claims",
		tag.NewStringTag("subject", claims.Subject),
		tag.NewInt("system_role", int(claims.System)),
		tag.NewStringTag("namespaces", fmt.Sprintf("%v", claims.Namespaces)),
	)
}
