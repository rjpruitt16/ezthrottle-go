package ezthrottle

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// StepType represents the execution strategy for a step
type StepType string

const (
	// StepTypeFrugal executes on client first, only queues to EZThrottle on error (cost optimization)
	StepTypeFrugal StepType = "frugal"

	// StepTypePerformance executes immediately via EZThrottle (speed optimization)
	StepTypePerformance StepType = "performance"
)

// IdempotentStrategy represents the idempotent key generation strategy
type IdempotentStrategy string

const (
	// IdempotentStrategyHash - Backend generates deterministic hash (prevents duplicates) [DEFAULT]
	IdempotentStrategyHash IdempotentStrategy = "hash"

	// IdempotentStrategyUnique - SDK generates UUID (allows duplicates for polling/webhooks/scheduled jobs)
	IdempotentStrategyUnique IdempotentStrategy = "unique"
)

// Step represents a workflow step with fluent builder pattern
type Step struct {
	client *Client

	// Core fields
	url            string
	method         string
	headers        map[string]string
	body           string
	metadata       map[string]interface{}
	stepType       StepType
	idempotentStrat IdempotentStrategy
	idempotentKey  string

	// FRUGAL-specific
	fallbackOnError []int
	localTimeout    time.Duration

	// PERFORMANCE-specific
	webhooks       []Webhook
	webhookQuorum  int
	regions        []string
	regionPolicy   string
	executionMode  string
	retryPolicy    *RetryPolicy
	retryAt        int64

	// Workflow chaining
	fallbackStep      *Step
	fallbackTrigger   map[string]interface{}
	onSuccessStep     *Step
	onFailureStep     *Step
	onFailureTimeout  int
}

// NewStep creates a new Step builder
func NewStep(client *Client) *Step {
	return &Step{
		client:          client,
		method:          "GET",
		headers:         make(map[string]string),
		metadata:        make(map[string]interface{}),
		stepType:        StepTypeFrugal,
		idempotentStrat: IdempotentStrategyHash,
		fallbackOnError: []int{429, 500, 502, 503, 504},
		localTimeout:    30 * time.Second,
		webhookQuorum:   1,
		regionPolicy:    "fallback",
		executionMode:   "race",
	}
}

// Core builder methods

// URL sets the target URL
func (s *Step) URL(url string) *Step {
	s.url = url
	return s
}

// Method sets the HTTP method
func (s *Step) Method(method string) *Step {
	s.method = method
	return s
}

// Header adds a single header
func (s *Step) Header(key, value string) *Step {
	s.headers[key] = value
	return s
}

// Headers sets all headers at once
func (s *Step) Headers(headers map[string]string) *Step {
	s.headers = headers
	return s
}

// Body sets the request body
func (s *Step) Body(body string) *Step {
	s.body = body
	return s
}

// Metadata adds metadata
func (s *Step) Metadata(metadata map[string]interface{}) *Step {
	s.metadata = metadata
	return s
}

// Type sets the step execution type (FRUGAL or PERFORMANCE)
func (s *Step) Type(stepType StepType) *Step {
	s.stepType = stepType
	return s
}

// IdempotentKey sets a custom idempotent key
func (s *Step) IdempotentKey(key string) *Step {
	s.idempotentKey = key
	return s
}

// IdempotentStrategy sets the idempotent key generation strategy
func (s *Step) IdempotentStrategy(strategy IdempotentStrategy) *Step {
	s.idempotentStrat = strategy
	return s
}

// FRUGAL-specific methods

// FallbackOnError sets which error codes trigger forwarding to EZThrottle (FRUGAL only)
func (s *Step) FallbackOnError(codes []int) *Step {
	s.fallbackOnError = codes
	return s
}

// Timeout sets the local execution timeout (FRUGAL only)
func (s *Step) Timeout(timeout time.Duration) *Step {
	s.localTimeout = timeout
	return s
}

// PERFORMANCE-specific methods

// Webhooks sets the webhook configurations (PERFORMANCE only)
func (s *Step) Webhooks(webhooks []Webhook) *Step {
	s.webhooks = webhooks
	return s
}

// WebhookQuorum sets the minimum webhooks that must succeed (PERFORMANCE only)
func (s *Step) WebhookQuorum(quorum int) *Step {
	s.webhookQuorum = quorum
	return s
}

// Regions sets the regions for multi-region execution (PERFORMANCE only)
func (s *Step) Regions(regions []string) *Step {
	s.regions = regions
	return s
}

// RegionPolicy sets the region policy (PERFORMANCE only)
func (s *Step) RegionPolicy(policy string) *Step {
	s.regionPolicy = policy
	return s
}

// ExecutionMode sets the execution mode (PERFORMANCE only)
func (s *Step) ExecutionMode(mode string) *Step {
	s.executionMode = mode
	return s
}

// RetryPolicy sets the retry policy (PERFORMANCE only)
func (s *Step) RetryPolicy(policy *RetryPolicy) *Step {
	s.retryPolicy = policy
	return s
}

// RetryAt sets when the job can be retried
func (s *Step) RetryAt(timestamp int64) *Step {
	s.retryAt = timestamp
	return s
}

// Workflow chaining methods

// Fallback adds a fallback step with trigger
func (s *Step) Fallback(step *Step, triggerOnError []int, triggerOnTimeout int) *Step {
	s.fallbackStep = step
	s.fallbackTrigger = make(map[string]interface{})

	if len(triggerOnError) > 0 {
		s.fallbackTrigger["type"] = "on_error"
		s.fallbackTrigger["codes"] = triggerOnError
	} else if triggerOnTimeout > 0 {
		s.fallbackTrigger["type"] = "on_timeout"
		s.fallbackTrigger["timeout_ms"] = triggerOnTimeout
	}

	return s
}

// OnSuccess adds a step to execute on success
func (s *Step) OnSuccess(step *Step) *Step {
	s.onSuccessStep = step
	return s
}

// OnFailure adds a step to execute on failure
func (s *Step) OnFailure(step *Step) *Step {
	s.onFailureStep = step
	return s
}

// OnFailureTimeout sets the failure timeout
func (s *Step) OnFailureTimeout(timeoutMs int) *Step {
	s.onFailureTimeout = timeoutMs
	return s
}

// Execute executes the step based on its type
func (s *Step) Execute(ctx context.Context) (*QueueResponse, error) {
	if s.stepType == StepTypeFrugal {
		return s.executeFrugal(ctx)
	}
	return s.executePerformance(ctx)
}

// executeFrugal executes locally first, forwards to EZThrottle on error
func (s *Step) executeFrugal(ctx context.Context) (*QueueResponse, error) {
	// Try local execution first
	resp, err := s.client.Request(s.method, s.url, s.headers, s.body)
	if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Success! Fire on_success workflow asynchronously if present
		if s.onSuccessStep != nil {
			go s.onSuccessStep.Execute(context.Background())
		}

		// Return immediate response (converted to QueueResponse format)
		return &QueueResponse{
			Status:   "completed",
			JobID:    "frugal_local_" + uuid.New().String(),
			QueuedAt: time.Now().UnixMilli(),
		}, nil
	}

	// Check if error code matches fallback trigger
	shouldFallback := false
	if resp != nil {
		for _, code := range s.fallbackOnError {
			if resp.StatusCode == code {
				shouldFallback = true
				break
			}
		}
	}

	if !shouldFallback && err == nil {
		// Unhandled error code
		return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
	}

	// Forward to EZThrottle with full workflow
	return s.forwardToEZThrottle()
}

// executePerformance submits immediately to EZThrottle
func (s *Step) executePerformance(ctx context.Context) (*QueueResponse, error) {
	return s.forwardToEZThrottle()
}

// forwardToEZThrottle builds and submits the job to EZThrottle
func (s *Step) forwardToEZThrottle() (*QueueResponse, error) {
	req := s.buildSubmitJobRequest()
	return s.client.SubmitJob(req)
}

// buildSubmitJobRequest builds a SubmitJobRequest from the Step
func (s *Step) buildSubmitJobRequest() *SubmitJobRequest {
	req := &SubmitJobRequest{
		URL:               s.url,
		Method:            s.method,
		Headers:           s.headers,
		Body:              s.body,
		Metadata:          s.metadata,
		Webhooks:          s.webhooks,
		WebhookQuorum:     s.webhookQuorum,
		Regions:           s.regions,
		RegionPolicy:      s.regionPolicy,
		ExecutionMode:     s.executionMode,
		RetryPolicy:       s.retryPolicy,
		RetryAt:           s.retryAt,
	}

	// Handle idempotent key based on strategy
	if s.idempotentKey != "" {
		req.IdempotentKey = s.idempotentKey
	} else if s.idempotentStrat == IdempotentStrategyUnique {
		req.IdempotentKey = uuid.New().String()
	}
	// else: HASH strategy - let backend generate deterministic hash

	// Add fallback chain
	if s.fallbackStep != nil {
		fallbackReq := s.fallbackStep.buildSubmitJobRequest()
		// Add trigger to fallback metadata
		if s.fallbackTrigger != nil {
			if fallbackReq.Metadata == nil {
				fallbackReq.Metadata = make(map[string]interface{})
			}
			fallbackReq.Metadata["trigger"] = s.fallbackTrigger
		}
		req.FallbackJob = fallbackReq
	}

	// Add workflow chaining
	if s.onSuccessStep != nil {
		req.OnSuccess = s.onSuccessStep.buildSubmitJobRequest()
	}
	if s.onFailureStep != nil {
		req.OnFailure = s.onFailureStep.buildSubmitJobRequest()
	}
	if s.onFailureTimeout > 0 {
		req.OnFailureTimeoutMs = s.onFailureTimeout
	}

	return req
}
