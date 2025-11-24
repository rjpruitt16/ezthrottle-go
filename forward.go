package ezthrottle

import (
	"context"
	"fmt"
)

// ForwardRequest represents a request that should be forwarded to EZThrottle.
// Use this with ExecuteWithForwarding() to integrate EZThrottle into legacy code
// without rewriting error handling.
//
// Example:
//
//	func legacyPayment(orderID string) (*http.Response, *ForwardRequest, error) {
//	    resp, err := http.Post("https://api.stripe.com/charges", ...)
//	    if err != nil {
//	        return nil, &ForwardRequest{
//	            URL: "https://api.stripe.com/charges",
//	            Method: "POST",
//	            IdempotentKey: fmt.Sprintf("order_%s", orderID),
//	        }, nil
//	    }
//	    return resp, nil, nil
//	}
type ForwardRequest struct {
	URL             string
	Method          string
	Headers         map[string]string
	Body            string
	IdempotentKey   string
	Metadata        map[string]interface{}
	Webhooks        []Webhook
	Regions         []string
	FallbackOnError []int
	StepType        StepType
}

// ExecuteWithForwarding wraps a function that may return a ForwardRequest,
// automatically forwarding to EZThrottle when requested.
//
// This is the Go equivalent of Python's @auto_forward decorator.
//
// Example:
//
//	resp, err := ExecuteWithForwarding(client, func() (interface{}, *ForwardRequest, error) {
//	    return processPaymentLegacy("order_12345")
//	})
func ExecuteWithForwarding(
	client *Client,
	fn func() (interface{}, *ForwardRequest, error),
) (interface{}, error) {
	result, forwardReq, err := fn()

	if err != nil {
		return nil, err
	}

	if forwardReq != nil {
		// Auto-forward to EZThrottle
		step := NewStep(client).
			URL(forwardReq.URL).
			Method(forwardReq.Method).
			Headers(forwardReq.Headers).
			Body(forwardReq.Body).
			IdempotentKey(forwardReq.IdempotentKey).
			Metadata(forwardReq.Metadata).
			Webhooks(forwardReq.Webhooks).
			Regions(forwardReq.Regions)

		// Set step type (default to FRUGAL if not specified)
		if forwardReq.StepType != "" {
			step = step.Type(forwardReq.StepType)
		} else {
			step = step.Type(StepTypeFrugal)
		}

		// Set fallback on error codes if specified
		if len(forwardReq.FallbackOnError) > 0 {
			step = step.FallbackOnError(forwardReq.FallbackOnError)
		}

		queueResp, err := step.Execute(context.Background())
		if err != nil {
			return nil, fmt.Errorf("failed to forward to EZThrottle: %w", err)
		}

		return queueResp, nil
	}

	return result, nil
}
