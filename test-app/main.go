// EZThrottle Go SDK Integration Test App
//
// Single Gin server with:
// - Test endpoints (submit jobs, return immediately with job_id)
// - Webhook endpoint (receive results from EZThrottle)
// - Query endpoints (hurl tests poll for webhooks)
//
// Flow:
// 1. POST /test/xxx → Submit job → Return job_id immediately
// 2. EZThrottle executes job → Sends webhook to /webhook
// 3. Hurl test polls GET /webhooks/{job_id} until webhook arrives
//
// Deploy: fly launch --name ezthrottle-sdk-go
// Test: hurl tests/*.hurl --test

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	ezthrottle "github.com/rjpruitt16/ezthrottle-go"
)

// Configuration
var (
	API_KEY        = getEnv("EZTHROTTLE_API_KEY", "")
	EZTHROTTLE_URL = getEnv("EZTHROTTLE_URL", "https://ezthrottle.fly.dev")
	APP_URL        = getEnv("APP_URL", "https://ezthrottle-sdk-go.fly.dev")
	PORT           = getEnv("PORT", "8080")
)

// In-memory webhook store (thread-safe with mutex)
type WebhookData struct {
	ReceivedAt string                 `json:"received_at"`
	Data       map[string]interface{} `json:"data"`
}

var (
	webhookStore = make(map[string]*WebhookData)
	storeMutex   sync.RWMutex
)

// EZThrottle client
var client *ezthrottle.Client

func main() {
	// Initialize EZThrottle client
	client = ezthrottle.NewClient(
		API_KEY,
		ezthrottle.WithTracktTagsURL("https://tracktags.fly.dev"),
	)

	log.Println("🚀 EZThrottle SDK Test App")
	log.Printf("   EZThrottle: %s\n", EZTHROTTLE_URL)
	log.Printf("   App URL: %s\n", APP_URL)
	log.Printf("   Webhook URL: %s/webhook\n", APP_URL)

	// Setup Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// =============================================================================
	// WEBHOOK RECEIVER
	// =============================================================================

	r.POST("/webhook", handleWebhook)
	r.GET("/webhooks/:job_id", getWebhook)
	r.GET("/webhooks", listWebhooks)
	r.POST("/webhooks/reset", resetWebhooks)

	// =============================================================================
	// TEST ENDPOINTS (Return job_id immediately, don't wait for webhooks)
	// =============================================================================

	r.POST("/test/performance/basic", testPerformanceBasic)
	r.POST("/test/performance/racing", testPerformanceRacing)
	r.POST("/test/performance/fallback-chain", testPerformanceFallbackChain)
	r.POST("/test/workflow/on-success", testWorkflowOnSuccess)
	r.POST("/test/idempotent/hash", testIdempotentHash)
	r.POST("/test/idempotent/unique", testIdempotentUnique)
	r.POST("/test/frugal/local", testFrugalLocal)

	// =============================================================================
	// HEALTH & INFO
	// =============================================================================

	r.GET("/", handleRoot)
	r.GET("/health", handleHealth)

	// Start server
	log.Printf("✅ Test app running on :%s\n", PORT)
	if err := r.Run(":" + PORT); err != nil {
		log.Fatal(err)
	}
}

// =============================================================================
// WEBHOOK HANDLERS
// =============================================================================

func handleWebhook(c *gin.Context) {
	var payload map[string]interface{}
	if err := c.BindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	jobID, _ := payload["job_id"].(string)
	idempotentKey, _ := payload["idempotent_key"].(string)
	status, _ := payload["status"].(string)

	// Store webhook data
	storeMutex.Lock()
	webhookStore[jobID] = &WebhookData{
		ReceivedAt: time.Now().Format(time.RFC3339),
		Data:       payload,
	}
	storeMutex.Unlock()

	log.Printf("✅ Webhook: %s | key: %s | status: %s\n", jobID, idempotentKey, status)

	c.JSON(http.StatusOK, gin.H{
		"status": "received",
		"job_id": jobID,
	})
}

func getWebhook(c *gin.Context) {
	jobID := c.Param("job_id")

	storeMutex.RLock()
	webhook, exists := webhookStore[jobID]
	storeMutex.RUnlock()

	if exists {
		c.JSON(http.StatusOK, gin.H{
			"found":   true,
			"job_id":  jobID,
			"webhook": webhook,
		})
	} else {
		c.JSON(http.StatusNotFound, gin.H{"found": false})
	}
}

func listWebhooks(c *gin.Context) {
	storeMutex.RLock()
	defer storeMutex.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"count":    len(webhookStore),
		"webhooks": webhookStore,
	})
}

func resetWebhooks(c *gin.Context) {
	storeMutex.Lock()
	webhookStore = make(map[string]*WebhookData)
	storeMutex.Unlock()

	log.Println("🧹 Webhook store cleared")
	c.JSON(http.StatusOK, gin.H{"status": "cleared"})
}

// =============================================================================
// TEST ENDPOINTS
// =============================================================================

func testPerformanceBasic(c *gin.Context) {
	// Test 1: PERFORMANCE - Basic webhook delivery
	testKey := fmt.Sprintf("performance_basic_%s", uuid.New().String())
	log.Printf("[TEST] Generated testKey: %s\n", testKey)

	result, err := ezthrottle.NewStep(client).
		URL("https://httpbin.org/status/200?test=performance_basic").
		Method("GET").
		Type(ezthrottle.StepTypePerformance).
		Webhooks([]ezthrottle.Webhook{{URL: fmt.Sprintf("%s/webhook", APP_URL), HasQuorumVote: true}}).
		IdempotentKey(testKey).
		Execute(context.Background())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"test":           "performance_basic",
		"idempotent_key": testKey,
		"result":         result,
	})
}

func testPerformanceRacing(c *gin.Context) {
	// Test 2: PERFORMANCE - Multi-region racing
	testKey := fmt.Sprintf("performance_racing_%s", uuid.New().String())

	result, err := ezthrottle.NewStep(client).
		URL("https://httpbin.org/delay/1?test=performance_racing").
		Method("GET").
		Type(ezthrottle.StepTypePerformance).
		Regions([]string{"iad", "lax", "ord"}).
		ExecutionMode("race").
		RegionPolicy("fallback").
		Webhooks([]ezthrottle.Webhook{{URL: fmt.Sprintf("%s/webhook", APP_URL), HasQuorumVote: true}}).
		IdempotentKey(testKey).
		Execute(context.Background())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"test":           "performance_racing",
		"idempotent_key": testKey,
		"result":         result,
	})
}

func testPerformanceFallbackChain(c *gin.Context) {
	// Test 3: PERFORMANCE - Fallback chain (OnError → OnTimeout)
	testKey := fmt.Sprintf("fallback_chain_%s", uuid.New().String())

	fallback2 := ezthrottle.NewStep(client).
		URL("https://httpbin.org/status/200?test=fallback_2").
		Method("GET")

	fallback1 := ezthrottle.NewStep(client).
		URL("https://httpbin.org/delay/2?test=fallback_1").
		Method("GET").
		Fallback(fallback2, []int{}, 500) // OnTimeout: 500ms

	result, err := ezthrottle.NewStep(client).
		URL("https://httpbin.org/status/500?test=fallback_primary").
		Method("GET").
		Type(ezthrottle.StepTypePerformance).
		Webhooks([]ezthrottle.Webhook{{URL: fmt.Sprintf("%s/webhook", APP_URL), HasQuorumVote: true}}).
		Fallback(fallback1, []int{500, 502, 503}, 0). // OnError
		IdempotentKey(testKey).
		Execute(context.Background())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"test":           "performance_fallback_chain",
		"idempotent_key": testKey,
		"result":         result,
	})
}

func testWorkflowOnSuccess(c *gin.Context) {
	// Test 4: PERFORMANCE - on_success workflow
	parentKey := fmt.Sprintf("on_success_parent_%s", uuid.New().String())
	childKey := fmt.Sprintf("on_success_child_%s", uuid.New().String())

	childStep := ezthrottle.NewStep(client).
		URL("https://httpbin.org/delay/1?test=on_success_child").
		Method("GET").
		Type(ezthrottle.StepTypePerformance).
		Webhooks([]ezthrottle.Webhook{{URL: fmt.Sprintf("%s/webhook", APP_URL), HasQuorumVote: true}}).
		IdempotentKey(childKey)

	result, err := ezthrottle.NewStep(client).
		URL("https://httpbin.org/status/200?test=on_success_parent").
		Method("GET").
		Type(ezthrottle.StepTypePerformance).
		Webhooks([]ezthrottle.Webhook{{URL: fmt.Sprintf("%s/webhook", APP_URL), HasQuorumVote: true}}).
		OnSuccess(childStep).
		IdempotentKey(parentKey).
		Execute(context.Background())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"test":       "workflow_on_success",
		"parent_key": parentKey,
		"child_key":  childKey,
		"result":     result,
	})
}

func testIdempotentHash(c *gin.Context) {
	// Test 5: Idempotent Key - HASH strategy (dedupe)
	runID := uuid.New().String()
	url := fmt.Sprintf("https://httpbin.org/get?test=idempotent_hash&run=%s", runID)

	result1, err := ezthrottle.NewStep(client).
		URL(url).
		Method("GET").
		Type(ezthrottle.StepTypePerformance).
		Webhooks([]ezthrottle.Webhook{{URL: fmt.Sprintf("%s/webhook", APP_URL), HasQuorumVote: true}}).
		IdempotentStrategy(ezthrottle.IdempotentStrategyHash).
		Execute(context.Background())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result2, err := ezthrottle.NewStep(client).
		URL(url).
		Method("GET").
		Type(ezthrottle.StepTypePerformance).
		Webhooks([]ezthrottle.Webhook{{URL: fmt.Sprintf("%s/webhook", APP_URL), HasQuorumVote: true}}).
		IdempotentStrategy(ezthrottle.IdempotentStrategyHash).
		Execute(context.Background())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"test":     "idempotent_hash",
		"result1":  result1,
		"result2":  result2,
		"expected": "Same job_id (deduped)",
		"deduped":  result1.JobID == result2.JobID,
	})
}

func testIdempotentUnique(c *gin.Context) {
	// Test 6: Idempotent Key - UNIQUE strategy (allow duplicates)
	key1 := fmt.Sprintf("idempotent_unique_1_%s", uuid.New().String())
	key2 := fmt.Sprintf("idempotent_unique_2_%s", uuid.New().String())

	result1, err := ezthrottle.NewStep(client).
		URL("https://httpbin.org/get?test=idempotent_unique").
		Method("GET").
		Type(ezthrottle.StepTypePerformance).
		Webhooks([]ezthrottle.Webhook{{URL: fmt.Sprintf("%s/webhook", APP_URL), HasQuorumVote: true}}).
		IdempotentKey(key1).
		Execute(context.Background())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result2, err := ezthrottle.NewStep(client).
		URL("https://httpbin.org/get?test=idempotent_unique").
		Method("GET").
		Type(ezthrottle.StepTypePerformance).
		Webhooks([]ezthrottle.Webhook{{URL: fmt.Sprintf("%s/webhook", APP_URL), HasQuorumVote: true}}).
		IdempotentKey(key2).
		Execute(context.Background())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"test":      "idempotent_unique",
		"key1":      key1,
		"key2":      key2,
		"result1":   result1,
		"result2":   result2,
		"expected":  "Different job_ids",
		"different": result1.JobID != result2.JobID,
	})
}

func testFrugalLocal(c *gin.Context) {
	// Test 7: FRUGAL - Local execution
	result, err := ezthrottle.NewStep(client).
		URL("https://httpbin.org/status/200?test=frugal_local").
		Type(ezthrottle.StepTypeFrugal).
		Execute(context.Background())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"test":   "frugal_local",
		"result": result,
	})
}

// =============================================================================
// HEALTH & INFO
// =============================================================================

func handleRoot(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service": "EZThrottle Go SDK Test App",
		"version": "1.1.0",
		"endpoints": gin.H{
			"tests": []string{
				"POST /test/performance/basic",
				"POST /test/performance/racing",
				"POST /test/performance/fallback-chain",
				"POST /test/workflow/on-success",
				"POST /test/idempotent/hash",
				"POST /test/idempotent/unique",
				"POST /test/frugal/local",
			},
			"webhooks": []string{
				"POST /webhook",
				"GET /webhooks/{job_id}",
				"GET /webhooks",
				"POST /webhooks/reset",
			},
		},
		"config": gin.H{
			"ezthrottle_url":      EZTHROTTLE_URL,
			"app_url":             APP_URL,
			"webhook_url":         fmt.Sprintf("%s/webhook", APP_URL),
			"api_key_configured": API_KEY != "",
		},
		"flow": gin.H{
			"1": "POST /test/xxx → Submit job → Return job_id",
			"2": "EZThrottle executes → Sends webhook to /webhook",
			"3": "Hurl polls GET /webhooks/{job_id} until webhook arrives",
		},
	})
}

func handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

// =============================================================================
// HELPERS
// =============================================================================

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
