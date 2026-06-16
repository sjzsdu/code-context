package store

import (
	"context"
	"reflect"
	"testing"
)

func TestDetectCapabilitiesFromInterfaces(t *testing.T) {
	provider := capabilityTestProvider{}
	got := DetectCapabilities(provider)
	want := []Capability{
		CapabilityGraphTraversal,
		CapabilityTextSearch,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectCapabilities() = %#v, want %#v", got, want)
	}
}

func TestDetectCapabilitiesMergesReporter(t *testing.T) {
	provider := capabilityReporterProvider{}
	got := DetectCapabilities(provider)
	want := []Capability{
		CapabilityHybridSearch,
		CapabilityTextSearch,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectCapabilities() = %#v, want %#v", got, want)
	}
}

func TestDetectCapabilitiesNil(t *testing.T) {
	if got := DetectCapabilities(nil); got != nil {
		t.Fatalf("DetectCapabilities(nil) = %#v, want nil", got)
	}
}

func TestParseTargetRef(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want TargetRef
	}{
		{
			name: "document path",
			raw:  "docs/health.md:12",
			want: TargetRef{Kind: TargetDocument, Path: "docs/health.md", Line: 12},
		},
		{
			name: "file prefix",
			raw:  "file:internal/server/server.go",
			want: TargetRef{Kind: TargetFile, Path: "internal/server/server.go"},
		},
		{
			name: "symbol with path",
			raw:  "symbol:HealthHandler@cmd/api/main.go:18",
			want: TargetRef{Kind: TargetSymbol, Name: "HealthHandler", Path: "cmd/api/main.go", Line: 18},
		},
		{
			name: "route",
			raw:  "GET /health",
			want: TargetRef{Kind: TargetRoute, Method: "GET", RoutePath: "/health", Value: "GET /health"},
		},
		{
			name: "bare symbol",
			raw:  "HealthHandler",
			want: TargetRef{Kind: TargetSymbol, Name: "HealthHandler"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseTargetRef(tt.raw); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseTargetRef(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}

type capabilityTestProvider struct{}

func (capabilityTestProvider) SearchText(context.Context, TextSearchQuery) ([]SearchHit, error) {
	return nil, nil
}

func (capabilityTestProvider) TraverseGraph(context.Context, GraphTraversalQuery) (*GraphTraversalResult, error) {
	return nil, nil
}

type capabilityReporterProvider struct{}

func (capabilityReporterProvider) Capabilities() []Capability {
	return []Capability{CapabilityHybridSearch, CapabilityTextSearch}
}
