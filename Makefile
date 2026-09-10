.PHONY: all build test up down tui demo attack kill-drill clean

all: build test

build:
	@echo "==> Building AegisPulse binaries..."
	@mkdir -p bin
	go build -o bin/gateway ./cmd/gateway
	go build -o bin/tui ./cmd/tui
	go build -o bin/mockupstream ./cmd/mockupstream
	go build -o bin/mockwebhook ./cmd/mockwebhook
	go build -o bin/chaos ./cmd/chaos
	@echo "==> Build complete."

test:
	@echo "==> Running unit test suite..."
	go test -v -race -timeout 30s ./...

up:
	@echo "==> Starting AegisPulse stack in Docker Compose..."
	docker compose up -d --build
	@echo "==> Waiting for services to pass health checks..."
	@sleep 4
	@echo "==> Stack is ready! Gateway: http://localhost:8080 | Grafana: http://localhost:3000 (admin/admin)"

down:
	@echo "==> Stopping AegisPulse stack..."
	docker compose down -v

tui:
	@echo "==> Launching AegisPulse Cyber-Deck Cockpit..."
	go run ./cmd/tui -url="http://localhost:8080"

demo:
	@echo "==> Executing 60-second guided end-to-end demo..."
	@bash ./scripts/demo.sh http://localhost:8080 || powershell -File ./scripts/demo.ps1 -GatewayURL "http://localhost:8080"

attack:
	@echo "==> Running automated chaos load & replay attack suite..."
	@bash ./scripts/attack.sh http://localhost:8080 || powershell -File ./scripts/attack.ps1 -GatewayURL "http://localhost:8080"

kill-drill:
	@echo "==> Testing two-step panic modal kill-switch drill..."
	@bash ./scripts/kill_drill.sh http://localhost:8080 || powershell -File ./scripts/kill_drill.ps1 -GatewayURL "http://localhost:8080"

clean:
	@echo "==> Cleaning build artifacts..."
	rm -rf bin/
