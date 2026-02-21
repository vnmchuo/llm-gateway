package proxy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sony/gobreaker"
	"github.com/vnmchuo/llm-gateway/internal/provider"
)

type Router struct {
	providers []provider.Provider
	breakers  map[string]*gobreaker.CircuitBreaker
}

func NewRouter(providers []provider.Provider) *Router {
	breakers := make(map[string]*gobreaker.CircuitBreaker)
	for _, p := range providers {
		settings := gobreaker.Settings{
			Name:        p.Name(),
			MaxRequests: 3,
			Interval:    5 * time.Second,
			Timeout:     30 * time.Second,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures >= 3
			},
		}
		breakers[p.Name()] = gobreaker.NewCircuitBreaker(settings)
	}
	return &Router{
		providers: providers,
		breakers:  breakers,
	}
}

func (r *Router) Route(ctx context.Context, req *provider.Request) (provider.Provider, error) {
	var candidates []provider.Provider
	for _, p := range r.providers {
		cb := r.breakers[p.Name()]
		if cb.State() == gobreaker.StateOpen {
			continue
		}

		if req.Model != "" {
			for _, m := range p.SupportedModels() {
				if m == req.Model {
					candidates = append(candidates, p)
					break
				}
			}
		} else {
			candidates = append(candidates, p)
		}
	}

	if len(candidates) == 0 {
		return nil, errors.New("all providers unavailable")
	}

	if req.Model != "" {
		return candidates[0], nil
	}

	best := candidates[0]
	for _, p := range candidates[1:] {
		if p.CostPerInputToken() < best.CostPerInputToken() {
			best = p
		}
	}
	return best, nil
}

func (r *Router) Execute(ctx context.Context, req *provider.Request, p provider.Provider) (*provider.Response, error) {
	cb := r.breakers[p.Name()]
	result, err := cb.Execute(func() (interface{}, error) {
		return p.Complete(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	return result.(*provider.Response), nil
}

func (r *Router) ExecuteStream(ctx context.Context, req *provider.Request, p provider.Provider) (<-chan *provider.Chunk, error) {
	cb := r.breakers[p.Name()]
	if cb.State() == gobreaker.StateOpen {
		return nil, fmt.Errorf("circuit breaker is open for provider: %s", p.Name())
	}

	origCh, err := p.CompleteStream(ctx, req)
	if err != nil {
		_, _ = cb.Execute(func() (interface{}, error) {
			return nil, err
		})
		return nil, err
	}

	wrappedCh := make(chan *provider.Chunk)
	go func() {
		defer close(wrappedCh)
		for chunk := range origCh {
			if chunk.Err != nil {
				_, _ = cb.Execute(func() (interface{}, error) {
					return nil, chunk.Err
				})
			}
			select {
			case wrappedCh <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return wrappedCh, nil
}
