# Test 7: FRUGAL - Local execution

POST {{APP_URL}}/test/frugal/local
HTTP 200
[Asserts]
jsonpath "$.result.status" == "success"
jsonpath "$.result.executed_locally" == true
jsonpath "$.result.status_code" == 200
