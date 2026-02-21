package provider

import (
	"context"
)

type Request struct {
	Model       string
	Messages    []Message
	MaxTokens   int
	Temperature float64
	Stream      bool
	// Metadata for routing decisions
	TenantID    string
	RequestID   string
}

type Message struct {
	Role    string // "user", "assistant", "system"
	Content string
}

type Response struct {
	ID           string
	Content      string
	InputTokens  int
	OutputTokens int
	Model        string
	Provider     string
	LatencyMs    int64
}

type Chunk struct {
	Delta string
	Done  bool
	Err   error
}

type Provider interface {
	Complete(ctx context.Context, req *Request) (*Response, error)
	CompleteStream(ctx context.Context, req *Request) (<-chan *Chunk, error)
	Name() string
	CostPerInputToken() float64 // cost in USD per 1 token
	CostPerOutputToken() float64
	SupportedModels() []string
}
