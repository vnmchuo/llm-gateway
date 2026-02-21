package proxy

import (
	"context"
	"errors"
	"testing"

	"github.com/vnmchuo/llm-gateway/internal/provider"
)

type MockProvider struct {
	name             string
	cost             float64
	supportedModels  []string
	completeErr      error
}

func (m *MockProvider) Complete(ctx context.Context, req *provider.Request) (*provider.Response, error) {
	if m.completeErr != nil {
		return nil, m.completeErr
	}
	return &provider.Response{
		Content:      "mock",
		Provider:     m.name,
		Model:        req.Model,
		InputTokens:  10,
		OutputTokens: 20,
	}, nil
}

func (m *MockProvider) CompleteStream(ctx context.Context, req *provider.Request) (<-chan *provider.Chunk, error) {
	ch := make(chan *provider.Chunk, 1)
	if m.completeErr != nil {
		ch <- &provider.Chunk{Err: m.completeErr}
	} else {
		ch <- &provider.Chunk{Delta: "mock", Done: true}
	}
	close(ch)
	return ch, nil
}

func (m *MockProvider) Name() string { return m.name }
func (m *MockProvider) CostPerInputToken() float64 { return m.cost }
func (m *MockProvider) CostPerOutputToken() float64 { return 0 }
func (m *MockProvider) SupportedModels() []string { return m.supportedModels }

func TestRoute_CostBased(t *testing.T) {
	p1 := &MockProvider{name: "expensive", cost: 10.0}
	p2 := &MockProvider{name: "cheap", cost: 1.0}
	
	router := NewRouter([]provider.Provider{p1, p2})
	
	p, err := router.Route(context.Background(), &provider.Request{})
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if p.Name() != "cheap" {
		t.Errorf("Expected cheap provider, got %s", p.Name())
	}
}

func TestRoute_ModelSpecific(t *testing.T) {
	p1 := &MockProvider{name: "gpt4-provider", supportedModels: []string{"gpt-4"}}
	p2 := &MockProvider{name: "claude-provider", supportedModels: []string{"claude-3"}}
	
	router := NewRouter([]provider.Provider{p1, p2})
	
	p, err := router.Route(context.Background(), &provider.Request{Model: "claude-3"})
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if p.Name() != "claude-provider" {
		t.Errorf("Expected claude-provider, got %s", p.Name())
	}
}

func TestRoute_CircuitBreakerOpen(t *testing.T) {
	p1 := &MockProvider{name: "bad-provider", cost: 0.1, completeErr: errors.New("fail")}
	p2 := &MockProvider{name: "good-provider", cost: 1.0}
	
	router := NewRouter([]provider.Provider{p1, p2})
	
	// Trip p1
	for i := 0; i < 3; i++ {
		router.Execute(context.Background(), &provider.Request{}, p1)
	}
	
	// p1 should now be excluded even if cheaper
	p, err := router.Route(context.Background(), &provider.Request{})
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if p.Name() != "good-provider" {
		t.Errorf("Expected good-provider because bad-provider should be tripped, got %s", p.Name())
	}
}

func TestRoute_AllProvidersDown(t *testing.T) {
	p1 := &MockProvider{name: "p1", completeErr: errors.New("fail")}
	
	router := NewRouter([]provider.Provider{p1})
	
	for i := 0; i < 3; i++ {
		router.Execute(context.Background(), &provider.Request{}, p1)
	}
	
	_, err := router.Route(context.Background(), &provider.Request{})
	if err == nil || err.Error() != "all providers unavailable" {
		t.Errorf("Expected 'all providers unavailable' error, got %v", err)
	}
}
