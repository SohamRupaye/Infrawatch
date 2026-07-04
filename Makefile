.PHONY: prod-up prod-down demo demo-down clean

# Production Deployment
prod-up:
	@echo "Starting production environment..."
	docker-compose -f docker/docker-compose.yml up -d --build

prod-down:
	@echo "Stopping production environment..."
	docker-compose -f docker/docker-compose.yml down

# Demo Environment
demo:
	@echo "Starting demo environment..."
	docker-compose -f docker/docker-compose.yml -f test/docker-compose.demo.yml up -d --build
	@echo "Demo environment started!"
	@echo "Frontend: http://localhost:3000"
	@echo "Test breaking API: http://localhost:4000/break/auth"

demo-down:
	@echo "Stopping demo environment..."
	docker-compose -f docker/docker-compose.yml -f test/docker-compose.demo.yml down

# Cleanup
clean:
	@echo "Cleaning up containers and volumes..."
	docker-compose -f docker/docker-compose.yml -f test/docker-compose.demo.yml down -v
	@echo "Cleaned."
