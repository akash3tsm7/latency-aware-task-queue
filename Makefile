.PHONY: help redis-up monitoring-up monitoring-down scheduler worker clean metrics

help:
	@echo "Latency-Aware Task Queue - Available Commands:"
	@echo "  make redis-up         - Start Redis"
	@echo "  make monitoring-up    - Start Prometheus & Grafana"
	@echo "  make monitoring-down  - Stop monitoring stack"
	@echo "  make scheduler        - Run scheduler"
	@echo "  make worker           - Run worker (CPU)"
	@echo "  make worker-gpu       - Run worker (GPU)"
	@echo "  make metrics          - Show current metrics"
	@echo "  make clean            - Clean up all containers"

redis-up:
	@echo "🚀 Starting Redis..."
	docker-compose up -d redis
	@echo "✅ Redis started on localhost:6379"

monitoring-up:
	@echo "🚀 Starting Prometheus & Grafana..."
	docker-compose up -d prometheus grafana
	@echo "✅ Prometheus: http://localhost:9090"
	@echo "✅ Grafana: http://localhost:3000 (admin/admin)"

monitoring-down:
	@echo "🛑 Stopping monitoring stack..."
	docker-compose stop prometheus grafana

scheduler:
	@echo "🚀 Starting scheduler..."
	go run cmd/scheduler/main.go cmd/scheduler/recovery.go

worker:
	@echo "🚀 Starting CPU worker..."
	go run cmd/worker/main.go

worker-gpu:
	@echo "🚀 Starting GPU worker..."
	@echo "Note: Update worker type to 'gpu' in cmd/worker/main.go"
	go run cmd/worker/main.go

metrics:
	@echo "📊 Scheduler Metrics:"
	@curl -s http://localhost:2112/metrics | grep latq_ | grep -v "#"
	@echo ""
	@echo "📊 Worker Metrics:"
	@curl -s http://localhost:2113/metrics | grep latq_ | grep -v "#"

clean:
	@echo "🧹 Cleaning up..."
	docker-compose down -v
	@echo "✅ All containers stopped and volumes removed"