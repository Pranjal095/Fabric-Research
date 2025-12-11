/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package endorser

import (
	"fmt"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	CircuitClosed   CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// CircuitBreakerConfig contains configuration for the circuit breaker
type CircuitBreakerConfig struct {
	Threshold     int
	Timeout       time.Duration
	MaxRetries    int
	RetryInterval time.Duration
}

// DefaultCircuitBreakerConfig returns default circuit breaker configuration
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Threshold:     5,
		Timeout:       30 * time.Second,
		MaxRetries:    3,
		RetryInterval: 5 * time.Second,
	}
}

// CircuitBreaker implements a circuit breaker pattern
type CircuitBreaker struct {
	failures        int
	lastFailureTime time.Time
	config          CircuitBreakerConfig
	state           CircuitState
	mu              sync.RWMutex
	metrics         *Metrics
	retryCount      int
}

// NewCircuitBreaker creates a new circuit breaker instance
func NewCircuitBreaker(config CircuitBreakerConfig, metrics *Metrics) *CircuitBreaker {
	return &CircuitBreaker{
		config:  config,
		state:   CircuitClosed,
		metrics: metrics,
	}
}

// Execute wraps an operation with circuit breaker logic
func (cb *CircuitBreaker) Execute(operation func() error) error {
	cb.mu.RLock()
	if cb.state == CircuitOpen {
		if time.Since(cb.lastFailureTime) < cb.config.Timeout {
			cb.mu.RUnlock()
			if cb.metrics != nil {
				cb.metrics.LeaderCircuitBreakerOpen.Add(1)
			}
			return fmt.Errorf("circuit breaker is open")
		}
		cb.mu.RUnlock()
		cb.mu.Lock()
		cb.state = CircuitHalfOpen
		cb.retryCount = 0
		cb.mu.Unlock()
		if cb.metrics != nil {
			cb.metrics.LeaderCircuitBreakerHalfOpen.Add(1)
		}
	} else {
		cb.mu.RUnlock()
	}

	err := operation()
	if err != nil {
		cb.mu.Lock()
		cb.failures++
		if cb.state == CircuitHalfOpen {
			cb.state = CircuitOpen
			cb.lastFailureTime = time.Now()
			if cb.metrics != nil {
				cb.metrics.LeaderCircuitBreakerOpen.Add(1)
			}
		} else if cb.failures >= cb.config.Threshold {
			cb.state = CircuitOpen
			cb.lastFailureTime = time.Now()
			if cb.metrics != nil {
				cb.metrics.LeaderCircuitBreakerOpen.Add(1)
			}
		}
		cb.mu.Unlock()
		return err
	}

	cb.mu.Lock()
	cb.failures = 0
	cb.state = CircuitClosed
	cb.mu.Unlock()
	if cb.metrics != nil {
		cb.metrics.LeaderCircuitBreakerClosed.Add(1)
	}
	return nil
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}
