package ezthrottle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client represents an EZThrottle SDK client
type Client struct {
	apiKey        string
	tracktTagsURL string
	httpClient    *http.Client
}

// NewClient creates a new EZThrottle client
func NewClient(apiKey string, opts ...ClientOption) *Client {
	c := &Client{
		apiKey:        apiKey,
		tracktTagsURL: "https://tracktags.fly.dev",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// ClientOption is a functional option for configuring the client
type ClientOption func(*Client)

// WithTracktTagsURL sets a custom TracktTags URL
func WithTracktTagsURL(url string) ClientOption {
	return func(c *Client) {
		c.tracktTagsURL = url
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// QueueRequest queues a request through EZThrottle
func (c *Client) QueueRequest(req *QueueRequest) (*QueueResponse, error) {
	// Build the job payload
	jobPayload := map[string]interface{}{
		"url":         req.URL,
		"method":      req.Method,
		"webhook_url": req.WebhookURL,
	}

	if req.Headers != nil {
		jobPayload["headers"] = req.Headers
	}
	if req.Body != "" {
		jobPayload["body"] = req.Body
	}
	if req.Metadata != nil {
		jobPayload["metadata"] = req.Metadata
	}
	if req.RetryAt > 0 {
		jobPayload["retry_at"] = req.RetryAt
	}

	// Build proxy payload
	proxyPayload := map[string]interface{}{
		"url":    "https://ezthrottle-staging.fly.dev/api/v1/jobs",
		"method": "POST",
		"headers": map[string]string{
			"Content-Type": "application/json",
		},
		"body": jobPayload,
	}

	payloadBytes, err := json.Marshal(proxyPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Make request to TracktTags proxy
	httpReq, err := http.NewRequest("POST", c.tracktTagsURL+"/api/v1/proxy", bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Handle rate limiting
	if resp.StatusCode == 429 {
		var errResp ErrorResponse
		if err := json.Unmarshal(bodyBytes, &errResp); err == nil {
			// Extract Retry-After header if present
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				// Convert seconds to milliseconds timestamp
				var seconds int64
				fmt.Sscanf(retryAfter, "%d", &seconds)
				errResp.RetryAt = time.Now().UnixMilli() + (seconds * 1000)
			}
			return nil, &EZThrottleError{
				Message: errResp.Error,
				RetryAt: errResp.RetryAt,
			}
		}
		return nil, &EZThrottleError{Message: "Rate limited"}
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("proxy request failed: %s", string(bodyBytes))
	}

	// Parse proxy response
	var proxyResp ProxyResponse
	if err := json.Unmarshal(bodyBytes, &proxyResp); err != nil {
		return nil, fmt.Errorf("failed to parse proxy response: %w", err)
	}

	if proxyResp.Status != "allowed" {
		return nil, fmt.Errorf("request denied: %s", proxyResp.Error)
	}

	// Parse forwarded EZThrottle response
	forwarded := proxyResp.ForwardedResponse
	if forwarded.StatusCode != 201 {
		return nil, fmt.Errorf("EZThrottle job creation failed: %s", forwarded.Body)
	}

	var queueResp QueueResponse
	if err := json.Unmarshal([]byte(forwarded.Body), &queueResp); err != nil {
		return nil, fmt.Errorf("failed to parse EZThrottle response: %w", err)
	}

	return &queueResp, nil
}

// Request makes a direct HTTP request (bypasses TracktTags and EZThrottle)
func (c *Client) Request(method, url string, headers map[string]string, body string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewReader([]byte(body))
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return c.httpClient.Do(req)
}

// QueueAndWait queues a request and polls for completion (blocking)
func (c *Client) QueueAndWait(req *QueueRequest, timeout time.Duration, pollInterval time.Duration) (map[string]interface{}, error) {
	resp, err := c.QueueRequest(req)
	if err != nil {
		return nil, err
	}

	// Start polling for webhook result
	start := time.Now()
	for time.Since(start) < timeout {
		time.Sleep(pollInterval)
		// User should implement their own webhook polling logic
		// This is just a placeholder
	}

	return nil, fmt.Errorf("timeout waiting for job %s", resp.JobID)
}

// Types

// QueueRequest represents a request to queue through EZThrottle
type QueueRequest struct {
	URL        string            `json:"url"`
	WebhookURL string            `json:"webhook_url"`
	Method     string            `json:"method,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	RetryAt    int64             `json:"retry_at,omitempty"` // Unix timestamp in milliseconds
}

// QueueResponse represents the response from queueing a job
type QueueResponse struct {
	JobID     string `json:"job_id"`
	Status    string `json:"status"`
	QueuedAt  int64  `json:"queued_at"`
	EstimatedProcessingTime int `json:"estimated_processing_time,omitempty"`
}

// ProxyResponse represents the TracktTags proxy response
type ProxyResponse struct {
	Status            string            `json:"status"`
	ForwardedResponse ForwardedResponse `json:"forwarded_response"`
	Error             string            `json:"error,omitempty"`
}

// ForwardedResponse represents the forwarded response from EZThrottle
type ForwardedResponse struct {
	StatusCode int               `json:"status_code"`
	Body       string            `json:"body"`
	Headers    map[string]string `json:"headers"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	RetryAt int64  `json:"retry_at,omitempty"`
}

// EZThrottleError represents an EZThrottle-specific error
type EZThrottleError struct {
	Message string
	RetryAt int64 // Unix timestamp in milliseconds when to retry
}

func (e *EZThrottleError) Error() string {
	if e.RetryAt > 0 {
		return fmt.Sprintf("%s (retry at: %d)", e.Message, e.RetryAt)
	}
	return e.Message
}
