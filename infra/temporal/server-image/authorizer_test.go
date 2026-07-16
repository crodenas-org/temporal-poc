package main

import (
	"context"
	"testing"

	"go.temporal.io/server/common/authorization"
)

// Claims fixtures matching the roles the Entra app actually issues.
var (
	claimsClusterAdmin = &authorization.Claims{System: authorization.RoleAdmin}
	claimsScopedReader = &authorization.Claims{
		Namespaces: map[string]authorization.Role{"default": authorization.RoleReader},
	}
	// A real state: authenticated against the tenant, but assigned no app roles.
	claimsNoGrant = &authorization.Claims{Subject: "nobody@example.com"}
)

func newTestAuthorizer() authorization.Authorizer {
	return newUIRenderAuthorizer(authorization.NewDefaultAuthorizer())
}

func allowed(t *testing.T, a authorization.Authorizer, claims *authorization.Claims, api string) bool {
	t.Helper()
	res, err := a.Authorize(context.Background(), claims, &authorization.CallTarget{
		APIName:   api,
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("Authorize(%s): %v", api, err)
	}
	return res.Decision == authorization.DecisionAllow
}

// TestUIRenderAuthorizer_UnblocksScopedUser is the point of the whole wrapper: a
// namespace-scoped human can render the UI, which the default authorizer denies.
func TestUIRenderAuthorizer_UnblocksScopedUser(t *testing.T) {
	wrapped := newTestAuthorizer()
	base := authorization.NewDefaultAuthorizer()

	for api := range uiRenderAPIs {
		t.Run(api, func(t *testing.T) {
			if allowed(t, base, claimsScopedReader, api) {
				t.Fatal("precondition failed: default authorizer already allows this; " +
					"the wrapper may no longer be needed")
			}
			if !allowed(t, wrapped, claimsScopedReader, api) {
				t.Error("scoped user still denied a UI render API — the UI will 403 and loop")
			}
		})
	}
}

// TestUIRenderAuthorizer_RequiresAGrant keeps namespace names from leaking to
// every authenticated identity in the directory.
func TestUIRenderAuthorizer_RequiresAGrant(t *testing.T) {
	a := newTestAuthorizer()

	for api := range uiRenderAPIs {
		t.Run("no grant/"+api, func(t *testing.T) {
			if allowed(t, a, claimsNoGrant, api) {
				t.Error("a role-less identity can enumerate the cluster")
			}
		})
		t.Run("nil claims/"+api, func(t *testing.T) {
			if allowed(t, a, nil, api) {
				t.Error("an unauthenticated caller can enumerate the cluster")
			}
		})
	}
}

// TestUIRenderAuthorizer_DoesNotWidenAnythingElse is the guard rail. The wrapper
// must be a keyhole: everything outside uiRenderAPIs has to decide exactly as the
// default authorizer would, or namespace isolation is silently compromised.
func TestUIRenderAuthorizer_DoesNotWidenAnythingElse(t *testing.T) {
	wrapped := newTestAuthorizer()
	base := authorization.NewDefaultAuthorizer()

	// Spans the axes that matter: namespace read/write, cluster-scoped admin, and
	// an operator-service call. None may be affected by the wrapper.
	otherAPIs := []string{
		workflowServicePrefix + "ListWorkflowExecutions",
		workflowServicePrefix + "StartWorkflowExecution",
		workflowServicePrefix + "TerminateWorkflowExecution",
		workflowServicePrefix + "DescribeNamespace",
		workflowServicePrefix + "RegisterNamespace",
		workflowServicePrefix + "GetSystemInfo", // ScopeCluster, deliberately NOT allowlisted
		"/temporal.api.operatorservice.v1.OperatorService/DeleteNamespace",
	}

	subjects := map[string]*authorization.Claims{
		"cluster admin":   claimsClusterAdmin,
		"scoped reader":   claimsScopedReader,
		"no grant":        claimsNoGrant,
		"unauthenticated": nil,
	}

	for name, claims := range subjects {
		for _, api := range otherAPIs {
			t.Run(name+"/"+api, func(t *testing.T) {
				want := allowed(t, base, claims, api)
				got := allowed(t, wrapped, claims, api)
				if got != want {
					t.Errorf("wrapper changed the decision: got allow=%v, default says allow=%v", got, want)
				}
			})
		}
	}
}

// TestUIRenderAuthorizer_ScopedReaderStillIsolated states the property the whole
// design exists to protect, at the authorizer level: rendering the UI must not
// hand a scoped user another namespace's data.
func TestUIRenderAuthorizer_ScopedReaderStillIsolated(t *testing.T) {
	a := newTestAuthorizer()

	res, err := a.Authorize(context.Background(), claimsScopedReader, &authorization.CallTarget{
		APIName:   workflowServicePrefix + "ListWorkflowExecutions",
		Namespace: "svc-demo", // NOT the namespace they hold a role on
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if res.Decision == authorization.DecisionAllow {
		t.Fatal("default:read reached workflows in svc-demo — namespace isolation is broken")
	}
}
