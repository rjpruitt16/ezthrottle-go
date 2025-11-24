.PHONY: integration deploy secrets teardown

# Integration tests (deploy + scale up + test + teardown)
integration:
	@echo "🚀 Starting integration test cycle..."
	@echo ""
	@echo "📦 STEP 1/4: Deploying test app to Fly..."
	@$(MAKE) deploy
	@echo ""
	@echo "⬆️  STEP 2/4: Scaling app up..."
	@fly scale count 1 --app ezthrottle-sdk-go --yes 2>&1 || echo "⚠️  Scale up failed"
	@echo "   Waiting 10s for app to start..."
	@sleep 10
	@echo "✅ App is running"
	@echo ""
	@echo "🧪 STEP 3/4: Running Hurl integration tests..."
	@hurl tests/*.hurl \
		--variable APP_URL=https://ezthrottle-sdk-go.fly.dev \
		--test \
		--color || (echo "❌ Tests failed! App left running for debugging" && exit 1)
	@echo ""
	@echo "✅ All tests passed!"
	@echo ""
	@echo "🔻 STEP 4/4: Scaling app to zero..."
	@$(MAKE) teardown
	@echo "✅ Integration tests complete! App scaled to zero (no cost)"

# Deploy to Fly.io (from parent directory to include SDK source)
deploy:
	@echo "   Listing Fly organizations..."
	@fly orgs list || echo "⚠️  Could not list orgs"

	@echo "   Listing Fly apps..."
	@fly apps list || echo "⚠️  Could not list apps"

	@echo "   Creating Fly app (if needed)..."
	@fly apps create ezthrottle-sdk-go --org ezthrottle 2>&1 || echo "⚠️  App create failed (might already exist)"

	@echo "   Deploying to Fly..."
	@cd .. && fly deploy \
		--config test-app/fly.toml \
		--dockerfile test-app/Dockerfile \
		--app ezthrottle-sdk-go \
		--ha=false

# Set secrets (run once after fly launch)
secrets:
	@bash -c 'source .env.test'

# Teardown (scale to zero to save resources without destroying the app)
teardown:
	@fly scale count 0 --app ezthrottle-sdk-go --yes 2>&1 || echo "⚠️  Scale to zero failed (app might not exist)"
	@echo "✅ App scaled to zero (costs nothing when idle)"
