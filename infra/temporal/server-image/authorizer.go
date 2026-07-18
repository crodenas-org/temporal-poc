package main

import (
	"context"

	"go.temporal.io/server/common/authorization"
)

// uiRenderAuthorizer lets a namespace-scoped human use the OSS Web UI.
//
// # Why this exists
//
// The UI's landing page calls a couple of cluster-scoped APIs to render at all.
// Temporal's default authorizer requires claims.System for those, which a scoped
// user (e.g. "default:read") does not have — so the page 403s and the UI bounces
// to /login, presenting as a login loop. That blocks the §1 goal of devs seeing
// their own namespace in the UI.
//
// This authorizer allows exactly the render APIs, and only for callers who
// already hold at least one Temporal grant. Every other decision — including all
// workflow data access — is delegated unchanged to the default authorizer, so
// namespace isolation is untouched.
//
// # The tradeoff, stated plainly
//
// ListNamespaces is allowed but NOT filtered, so any user with a grant can see
// the *names* of all namespaces in the switcher. No workflow data leaks: opening
// a namespace they lack a role for is still denied by the delegate. Filtering the
// response to the caller's namespaces needs a frontend gRPC interceptor
// (AUTHZ.md §15); deliberately deferred — name visibility was judged acceptable,
// and the interceptor carries real upgrade exposure.
//
// # Adding to the allowlist
//
// Extend uiRenderAPIs only from OBSERVED denials, never by guessing which APIs
// the UI "probably" needs — an over-broad allowlist silently widens cluster-level
// read access. Run with TEMPORAL_AUTH_DEBUG=1, sign in as a scoped user, and read
// the 403s out of the ui-server access log (AUTHZ.md §14 #13).

// workflowServicePrefix is the gRPC prefix api.GetMethodMetadata matches on;
// ListNamespaces and GetClusterInfo both live in workflowServiceMetadata.
const workflowServicePrefix = "/temporal.api.workflowservice.v1.WorkflowService/"

// uiRenderAPIs are the cluster-scoped calls the UI needs before it can show a
// namespace-scoped user anything. Measured, not assumed: these are the exact two
// denied for a "default:read" login (AUTHZ.md §14 #6). GetSystemInfo is also
// ScopeCluster but was NOT observed being denied, so it is deliberately absent.
var uiRenderAPIs = map[string]struct{}{
	workflowServicePrefix + "ListNamespaces": {},
	workflowServicePrefix + "GetClusterInfo": {},
}

type uiRenderAuthorizer struct {
	delegate authorization.Authorizer
}

// newUIRenderAuthorizer wraps delegate (in practice the default authorizer),
// widening only the UI render path.
func newUIRenderAuthorizer(delegate authorization.Authorizer) authorization.Authorizer {
	return &uiRenderAuthorizer{delegate: delegate}
}

var _ authorization.Authorizer = (*uiRenderAuthorizer)(nil)

func (a *uiRenderAuthorizer) Authorize(
	ctx context.Context,
	claims *authorization.Claims,
	target *authorization.CallTarget,
) (authorization.Result, error) {
	if _, isRenderAPI := uiRenderAPIs[target.APIName]; isRenderAPI && hasAnyGrant(claims) {
		return authorization.Result{Decision: authorization.DecisionAllow}, nil
	}
	return a.delegate.Authorize(ctx, claims, target)
}

// hasAnyGrant reports whether the caller holds at least one Temporal role.
//
// Authentication alone is not enough: every user in the Entra tenant can obtain a
// valid token, and assigning no app roles is a real state (it is what
// `make entra-assign` with no arguments produces). Requiring a grant keeps
// namespace names from being visible to the whole directory.
func hasAnyGrant(claims *authorization.Claims) bool {
	if claims == nil {
		return false
	}
	if claims.System != authorization.RoleUndefined {
		return true
	}
	for _, role := range claims.Namespaces {
		if role != authorization.RoleUndefined {
			return true
		}
	}
	return false
}
