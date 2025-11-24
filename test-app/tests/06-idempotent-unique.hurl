# Test 6: Idempotent UNIQUE - Allow duplicates

POST {{APP_URL}}/test/idempotent/unique
HTTP 200
[Asserts]
jsonpath "$.different" == true

[Captures]
job_id_1: jsonpath "$.result1.job_id"
job_id_2: jsonpath "$.result2.job_id"

# Poll for both webhooks
GET {{APP_URL}}/webhooks/{{job_id_1}}
[Options]
retry: 30
retry-interval: 1000
HTTP 200

GET {{APP_URL}}/webhooks/{{job_id_2}}
[Options]
retry: 30
retry-interval: 1000
HTTP 200
