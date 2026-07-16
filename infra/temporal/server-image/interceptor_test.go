package main

import (
	"context"
	"errors"
	"testing"

	namespacepb "go.temporal.io/api/namespace/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/server/common/authorization"
	"google.golang.org/grpc"
)

func nsList(names ...string) *workflowservice.ListNamespacesResponse {
	out := &workflowservice.ListNamespacesResponse{}
	for _, n := range names {
		out.Namespaces = append(out.Namespaces, &workflowservice.DescribeNamespaceResponse{
			NamespaceInfo: &namespacepb.NamespaceInfo{Name: n},
		})
	}
	return out
}

func namesOf(list *workflowservice.ListNamespacesResponse) []string {
	var out []string
	for _, ns := range list.Namespaces {
		out = append(out, ns.GetNamespaceInfo().GetName())
	}
	return out
}

func equalNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The cluster as the UI would see it unfiltered.
func fullCluster() *workflowservice.ListNamespacesResponse {
	return nsList("temporal-system", "default", "svc-demo")
}

func TestFilterNamespaces(t *testing.T) {
	tests := []struct {
		name   string
		claims *authorization.Claims
		want   []string
	}{
		{
			name:   "scoped reader sees only their namespace",
			claims: claimsScopedReader,
			want:   []string{"default"},
		},
		{
			name: "multiple grants are all kept",
			claims: &authorization.Claims{Namespaces: map[string]authorization.Role{
				"default":  authorization.RoleReader,
				"svc-demo": authorization.RoleWriter,
			}},
			want: []string{"default", "svc-demo"},
		},
		{
			name:   "cluster admin sees everything",
			claims: claimsClusterAdmin,
			want:   []string{"temporal-system", "default", "svc-demo"},
		},
		{
			name:   "cluster reader sees everything",
			claims: &authorization.Claims{System: authorization.RoleReader},
			want:   []string{"temporal-system", "default", "svc-demo"},
		},
		{
			name:   "a grant-less identity sees nothing",
			claims: claimsNoGrant,
			want:   nil,
		},
		{
			// Auth off, or an internal-frontend call that bypasses auth. Filtering
			// here would break plaintext dev and Temporal's own system workers.
			name:   "nil claims are not filtered",
			claims: nil,
			want:   []string{"temporal-system", "default", "svc-demo"},
		},
		{
			// System=Worker is below cluster-read, so it filters — and a worker
			// role on no namespace means an empty list, not a full one.
			name:   "system worker does not get the cluster view",
			claims: &authorization.Claims{System: authorization.RoleWorker},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := fullCluster()
			filterNamespaces(list, tt.claims)
			if got := namesOf(list); !equalNames(got, tt.want) {
				t.Errorf("namespaces = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestInterceptor_FiltersListNamespaces drives the interceptor as gRPC would,
// with claims in the context exactly as the internal auth interceptor leaves them.
func TestInterceptor_FiltersListNamespaces(t *testing.T) {
	intercept := newNamespaceFilterInterceptor()
	ctx := context.WithValue(context.Background(), authorization.MappedClaims, claimsScopedReader)

	resp, err := intercept(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: listNamespacesMethod},
		func(context.Context, any) (any, error) { return fullCluster(), nil },
	)
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	got := namesOf(resp.(*workflowservice.ListNamespacesResponse))
	if !equalNames(got, []string{"default"}) {
		t.Errorf("namespaces = %v, want [default]", got)
	}
}

// TestInterceptor_LeavesOtherCallsAlone keeps the interceptor a keyhole: it must
// touch nothing but ListNamespaces.
func TestInterceptor_LeavesOtherCallsAlone(t *testing.T) {
	intercept := newNamespaceFilterInterceptor()
	ctx := context.WithValue(context.Background(), authorization.MappedClaims, claimsScopedReader)

	// Same response type on a different method must pass through untouched.
	resp, err := intercept(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: workflowServicePrefix + "DescribeNamespace"},
		func(context.Context, any) (any, error) { return fullCluster(), nil },
	)
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if got := namesOf(resp.(*workflowservice.ListNamespacesResponse)); len(got) != 3 {
		t.Errorf("a non-ListNamespaces call was filtered: %v", got)
	}
}

// TestInterceptor_PropagatesHandlerError — on error there is no response to
// filter, and the error must reach the caller unchanged.
func TestInterceptor_PropagatesHandlerError(t *testing.T) {
	intercept := newNamespaceFilterInterceptor()
	want := errors.New("boom")

	_, err := intercept(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: listNamespacesMethod},
		func(context.Context, any) (any, error) { return nil, want },
	)
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

// TestInterceptor_PreservesPageToken documents the accepted pagination edge: the
// token is passed through untouched, so a filtered page can be short (or empty)
// while still pointing at more results.
func TestInterceptor_PreservesPageToken(t *testing.T) {
	list := nsList("svc-demo")
	list.NextPageToken = []byte("more")

	filterNamespaces(list, claimsScopedReader) // holds no role on svc-demo

	if len(list.Namespaces) != 0 {
		t.Fatalf("expected an empty page, got %v", namesOf(list))
	}
	if string(list.NextPageToken) != "more" {
		t.Error("next page token was dropped; the caller can no longer page on")
	}
}
