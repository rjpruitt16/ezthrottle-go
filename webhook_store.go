package ezthrottle

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// WebhookResult represents the webhook payload from EZThrottle
type WebhookResult struct {
	JobID         string                 `json:"job_id"`
	IdempotentKey string                 `json:"idempotent_key"`
	Status        string                 `json:"status"` // "success" or "failed"
	Response      WebhookResponse        `json:"response"`
	Metadata      map[string]interface{} `json:"metadata"`
	ReceivedAt    time.Time              `json:"received_at"`
}

// WebhookResponse represents the response from the target API
type WebhookResponse struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

// StatusUpdate represents a streaming status update
type StatusUpdate struct {
	State         string         // "queued", "completed", "failed"
	JobID         string         // Job ID from EZThrottle
	IdempotentKey string         // Idempotent key
	Result        *WebhookResult // Final webhook result (only for "completed")
	Error         error          // Error details (only for "failed")
}

// WebhookStore manages webhook results in-memory
type WebhookStore struct {
	mu       sync.RWMutex
	webhooks map[string]*WebhookResult
	watchers map[string][]chan *WebhookResult
}

// NewWebhookStore creates a new webhook store
func NewWebhookStore() *WebhookStore {
	return &WebhookStore{
		webhooks: make(map[string]*WebhookResult),
		watchers: make(map[string][]chan *WebhookResult),
	}
}

// Store saves a webhook result and notifies watchers
func (s *WebhookStore) Store(result *WebhookResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result.ReceivedAt = time.Now()
	// Store by both idempotent_key and job_id for flexible lookups
	s.webhooks[result.IdempotentKey] = result
	s.webhooks[result.JobID] = result

	// Notify all watchers for this idempotent key
	if watchers, exists := s.watchers[result.IdempotentKey]; exists {
		for _, ch := range watchers {
			select {
			case ch <- result:
			default:
				// Channel full or closed, skip
			}
		}
		// Clear watchers after notification
		delete(s.watchers, result.IdempotentKey)
	}

	// Also notify watchers for job_id
	if watchers, exists := s.watchers[result.JobID]; exists {
		for _, ch := range watchers {
			select {
			case ch <- result:
			default:
				// Channel full or closed, skip
			}
		}
		// Clear watchers after notification
		delete(s.watchers, result.JobID)
	}
}

// Get retrieves a webhook result by idempotent key
func (s *WebhookStore) Get(idempotentKey string) (*WebhookResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result, exists := s.webhooks[idempotentKey]
	return result, exists
}

// Watch registers a channel to be notified when a webhook arrives
func (s *WebhookStore) Watch(idempotentKey string) <-chan *WebhookResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan *WebhookResult, 1)

	// Check if result already exists
	if result, exists := s.webhooks[idempotentKey]; exists {
		ch <- result
		close(ch)
		return ch
	}

	// Register watcher
	s.watchers[idempotentKey] = append(s.watchers[idempotentKey], ch)

	return ch
}

// processWebhook handles incoming webhook HTTP requests
// This is used internally by the WebhookHandler
func (s *WebhookStore) processWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var result WebhookResult
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		log.Printf("Failed to decode webhook: %v\n", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Received webhook: job_id=%s, idempotent_key=%s, status=%s\n",
		result.JobID, result.IdempotentKey, result.Status)

	// Store webhook and notify watchers
	s.Store(&result)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
