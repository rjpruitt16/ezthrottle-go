# Test 1: PERFORMANCE - Basic webhook delivery

POST {{APP_URL}}/test/performance/basic
HTTP 200
[Captures]
job_id: jsonpath "$.result.job_id"
idempotent_key: jsonpath "$.idempotent_key"

# Poll for webhook (retry up to 30s)
GET {{APP_URL}}/webhooks/{{idempotent_key}}
[Options]
retry: 30
retry-interval: 1000
HTTP 200
[Asserts]
jsonpath "$.found" == true
jsonpath "$.webhook.data.status" == "success"
jsonpath "$.webhook.data.idempotent_key" == "{{idempotent_key}}"
