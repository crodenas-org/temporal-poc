package main

import (
	"context"

	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/server/common/authorization"
	"google.golang.org/grpc"
)

// namespaceFilterInterceptor trims the ListNamespaces response to the namespaces
// the caller actually holds a role on.
//
// # Why this is not cosmetic
//
// uiRenderAuthorizer (authorizer.go) allows ListNamespaces so a scoped human can
// render the UI at all — but allowing it unfiltered makes the namespace switcher
// advertise every namespace on the cluster. Clicking one you lack a role on
// returns 403, and the stock UI treats ANY 403 as "not authenticated": it
// redirects to /login, re-authenticates successfully, lands back on the same
// page, 403s again — an infinite login loop. Observed live (AUTHZ.md §14 #14).
//
// So the unfiltered list is a trap, not just a name leak: it invites users into a
// dead end. We do not build the UI (§4 keeps it stock), so its 403 handling is
// not ours to fix. Removing the invitation is. With the list filtered, a scoped
// dev never sees a namespace they cannot open and never loops.
//
// Deep-linking a forbidden URL by hand still loops. That is accepted: it is a
// rare path, and the alternative is forking the UI.
//
// # Why an interceptor and not the authorizer
//
// Authorizer.Authorize returns Allow/Deny for a call — it cannot alter a response
// body. Filtering has to happen after the handler runs. Custom frontend
// interceptors are chained AFTER Temporal's internal ones
// (temporal/server_option.go:193), so authorization has already run and
// authorization.MappedClaims is populated in the context by then.
//
// # Upgrade exposure
//
// This is the most upgrade-sensitive code we own: it touches an API response
// shape, whereas the authorizer only reads target.APIName. Both the response type
// and MappedClaims are pinned by the version in go.mod — re-check this on every
// Temporal bump (§10).

const listNamespacesMethod = workflowServicePrefix + "ListNamespaces"

// newNamespaceFilterInterceptor returns a unary interceptor for
// temporal.WithChainedFrontendGrpcInterceptors. Install it only when
// authorization is enabled: with no authorizer there are no claims to filter by,
// and plaintext dev should keep showing everything.
func newNamespaceFilterInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		resp, err := handler(ctx, req)
		if err != nil || info.FullMethod != listNamespacesMethod {
			return resp, err
		}
		list, ok := resp.(*workflowservice.ListNamespacesResponse)
		if !ok {
			// Response type changed under us — leave it alone rather than guess.
			return resp, err
		}
		claims, _ := ctx.Value(authorization.MappedClaims).(*authorization.Claims)
		filterNamespaces(list, claims)
		return list, nil
	}
}

// filterNamespaces removes namespaces the caller holds no role on. It mutates
// list in place; the response is freshly built per call.
//
// NOTE (pagination): filtering happens after the page was assembled, so a page
// can come back short — or empty with a non-nil NextPageToken. Callers that stop
// on an empty page could miss later ones. Harmless at our namespace count, and
// fixing it properly means re-driving the handler, which risks hammering
// persistence. Revisit if namespace count ever approaches a page.
func filterNamespaces(list *workflowservice.ListNamespacesResponse, claims *authorization.Claims) {
	if !shouldFilterNamespaces(claims) {
		return
	}
	kept := make([]*workflowservice.DescribeNamespaceResponse, 0, len(list.Namespaces))
	for _, ns := range list.Namespaces {
		if claims.Namespaces[ns.GetNamespaceInfo().GetName()] != authorization.RoleUndefined {
			kept = append(kept, ns)
		}
	}
	list.Namespaces = kept
}

// shouldFilterNamespaces reports whether this caller gets a trimmed list.
//
// Operators keep the full view: the bar is cluster-wide read, the same threshold
// the default authorizer itself applies to ListNamespaces (RoleReader against a
// ScopeCluster API), so "sees every namespace" and "is allowed to read across
// namespaces" stay the same statement.
//
// Nil claims mean no authorizer ran (auth off) or an internal-frontend call that
// bypasses auth — neither should be filtered.
func shouldFilterNamespaces(claims *authorization.Claims) bool {
	if claims == nil {
		return false
	}
	return claims.System < authorization.RoleReader
}
