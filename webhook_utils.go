package ezthrottle

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// VerificationResult represents the result of webhook signature verification
type VerificationResult struct {
	Verified bool
	Reason   string
}

// WebhookVerificationError represents an error in webhook verification
type WebhookVerificationError struct {
	Message string
}

func (e *WebhookVerificationError) Error() string {
	return e.Message
}

// VerifyWebhookSignature verifies HMAC-SHA256 signature from X-EZThrottle-Signature header.
//
// Parameters:
//   - payload: Raw webhook payload (request body as bytes)
//   - signatureHeader: Value of X-EZThrottle-Signature header
//   - secret: Your webhook secret (primary or secondary)
//   - tolerance: Maximum age of timestamp in seconds (default: 300 = 5 minutes)
//
// Returns:
//   - VerificationResult with Verified bool and Reason string
//
// Example:
//
//	func webhookHandler(w http.ResponseWriter, r *http.Request) {
//	    body, _ := io.ReadAll(r.Body)
//	    signature := r.Header.Get("X-EZThrottle-Signature")
//	    result := ezthrottle.VerifyWebhookSignature(body, signature, "your_secret", 300)
//
//	    if !result.Verified {
//	        http.Error(w, "Invalid signature: "+result.Reason, http.StatusUnauthorized)
//	        return
//	    }
//
//	    // Process webhook...
//	    w.WriteHeader(http.StatusOK)
//	}
func VerifyWebhookSignature(payload []byte, signatureHeader, secret string, tolerance int) VerificationResult {
	if signatureHeader == "" {
		return VerificationResult{Verified: false, Reason: "no_signature_header"}
	}

	// Parse "t=timestamp,v1=signature" format
	parts := make(map[string]string)
	for _, part := range strings.Split(signatureHeader, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			parts[kv[0]] = kv[1]
		}
	}

	timestampStr := parts["t"]
	signature := parts["v1"]

	if signature == "" {
		return VerificationResult{Verified: false, Reason: "missing_v1_signature"}
	}

	// Check timestamp tolerance
	now := time.Now().Unix()
	sigTime, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return VerificationResult{Verified: false, Reason: "invalid_timestamp"}
	}

	timeDiff := now - sigTime
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}

	if timeDiff > int64(tolerance) {
		return VerificationResult{
			Verified: false,
			Reason:   fmt.Sprintf("timestamp_expired (diff=%ds, tolerance=%ds)", timeDiff, tolerance),
		}
	}

	// Compute expected signature
	signedPayload := fmt.Sprintf("%s.%s", timestampStr, string(payload))
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(signedPayload))
	expected := hex.EncodeToString(h.Sum(nil))

	// Constant-time comparison
	if subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) == 1 {
		return VerificationResult{Verified: true, Reason: "valid"}
	}

	return VerificationResult{Verified: false, Reason: "signature_mismatch"}
}

// VerifyWebhookSignatureStrict verifies webhook signature and returns error if invalid.
//
// Parameters:
//   - payload: Raw webhook payload
//   - signatureHeader: Value of X-EZThrottle-Signature header
//   - secret: Your webhook secret
//   - tolerance: Maximum age of timestamp in seconds (default: 300)
//
// Returns:
//   - error if signature verification fails, nil otherwise
//
// Example:
//
//	func webhookHandler(w http.ResponseWriter, r *http.Request) {
//	    body, _ := io.ReadAll(r.Body)
//	    signature := r.Header.Get("X-EZThrottle-Signature")
//
//	    if err := ezthrottle.VerifyWebhookSignatureStrict(body, signature, "your_secret", 300); err != nil {
//	        http.Error(w, err.Error(), http.StatusUnauthorized)
//	        return
//	    }
//
//	    // Process webhook...
//	    w.WriteHeader(http.StatusOK)
//	}
func VerifyWebhookSignatureStrict(payload []byte, signatureHeader, secret string, tolerance int) error {
	result := VerifyWebhookSignature(payload, signatureHeader, secret, tolerance)
	if !result.Verified {
		return &WebhookVerificationError{
			Message: fmt.Sprintf("Webhook signature verification failed: %s", result.Reason),
		}
	}
	return nil
}

// TryVerifyWithSecrets tries verifying signature with primary secret, falls back to secondary if provided.
// Useful during secret rotation when you have both old and new secrets active.
//
// Parameters:
//   - payload: Raw webhook payload
//   - signatureHeader: Value of X-EZThrottle-Signature header
//   - primarySecret: Your primary webhook secret
//   - secondarySecret: Your secondary webhook secret (optional, use empty string if not provided)
//   - tolerance: Maximum age of timestamp in seconds
//
// Returns:
//   - VerificationResult with reason "valid_primary" or "valid_secondary" if verified
//
// Example:
//
//	// During secret rotation
//	result := ezthrottle.TryVerifyWithSecrets(
//	    body,
//	    signature,
//	    "new_secret_after_rotation",
//	    "old_secret_before_rotation",
//	    300,
//	)
//
//	if result.Verified {
//	    fmt.Printf("Signature verified with %s\n", result.Reason) // "valid_primary" or "valid_secondary"
//	}
func TryVerifyWithSecrets(payload []byte, signatureHeader, primarySecret, secondarySecret string, tolerance int) VerificationResult {
	// Try primary secret first
	primaryResult := VerifyWebhookSignature(payload, signatureHeader, primarySecret, tolerance)
	if primaryResult.Verified {
		return VerificationResult{Verified: true, Reason: "valid_primary"}
	}

	// Try secondary secret if provided
	if secondarySecret != "" {
		secondaryResult := VerifyWebhookSignature(payload, signatureHeader, secondarySecret, tolerance)
		if secondaryResult.Verified {
			return VerificationResult{Verified: true, Reason: "valid_secondary"}
		}
	}

	return VerificationResult{
		Verified: false,
		Reason:   fmt.Sprintf("both_secrets_failed (primary: %s)", primaryResult.Reason),
	}
}
