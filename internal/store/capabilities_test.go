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
