.PHONY: integration deploy secrets

# Integration tests (deploy + test via Hurl)
integration:
	@echo "🚀 Deploying test app to Fly..."
	@$(MAKE) deploy
	@echo ""
	@echo "⏳ Waiting 5s for deployment..."
	sleep 5
	@echo ""
	@echo "🧪 Running Hurl integration tests..."
	hurl tests/*.hurl \
		--variable APP_URL=https://ezthrottle-sdk-go.fly.dev \
		--test \
		--color
	@echo ""
	@echo "✅ All tests passed!"

# Deploy to Fly.io (from parent directory to include SDK source)
deploy:
	@cd .. && fly deploy \
		--config test-app/fly.toml \
		--dockerfile test-app/Dockerfile \
		--app ezthrottle-sdk-go \
		--ha=false

# Set secrets (run once after fly launch)
secrets:
	@bash -c 'source .env.test'
