#!/bin/bash

# Validation script for Prometheus metrics integration
# Usage: ./scripts/validate-metrics.sh

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

SCHEDULER_PORT=2112
WORKER_PORT=2113
PROMETHEUS_PORT=9090
GRAFANA_PORT=3000
REDIS_PORT=6379

echo "🔍 Validating Latency-Aware Task Queue Metrics Setup..."
echo ""

# Function to check if a port is listening
check_port() {
    local port=$1
    local service=$2
    
    if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1 || nc -z localhost $port 2>/dev/null; then
        echo -e "${GREEN}✓${NC} $service is running on port $port"
        return 0
    else
        echo -e "${RED}✗${NC} $service is NOT running on port $port"
        return 1
    fi
}

# Function to check HTTP endpoint
check_endpoint() {
    local url=$1
    local description=$2
    
    if curl -s -o /dev/null -w "%{http_code}" "$url" | grep -q "200"; then
        echo -e "${GREEN}✓${NC} $description: $url"
        return 0
    else
        echo -e "${RED}✗${NC} $description: $url (not accessible)"
        return 1
    fi
}

# Function to check metric exists
check_metric() {
    local url=$1
    local metric=$2
    local service=$3
    
    if curl -s "$url" | grep -q "$metric"; then
        echo -e "${GREEN}✓${NC} $service exports metric: $metric"
        return 0
    else
        echo -e "${RED}✗${NC} $service missing metric: $metric"
        return 1
    fi
}

# Track overall status
FAILED=0

echo "📡 Checking Services..."
echo "--------------------"
check_port $REDIS_PORT "Redis" || ((FAILED++))
check_port $SCHEDULER_PORT "Scheduler" || ((FAILED++))
check_port $WORKER_PORT "Worker" || ((FAILED++))
check_port $PROMETHEUS_PORT "Prometheus" || ((FAILED++))
check_port $GRAFANA_PORT "Grafana" || ((FAILED++))
echo ""

echo "🌐 Checking HTTP Endpoints..."
echo "----------------------------"
check_endpoint "http://localhost:$SCHEDULER_PORT/metrics" "Scheduler metrics" || ((FAILED++))
check_endpoint "http://localhost:$WORKER_PORT/metrics" "Worker metrics" || ((FAILED++))
check_endpoint "http://localhost:$PROMETHEUS_PORT/-/healthy" "Prometheus health" || ((FAILED++))
check_endpoint "http://localhost:$GRAFANA_PORT/api/health" "Grafana health" || ((FAILED++))
echo ""

echo "📊 Validating Scheduler Metrics..."
echo "--------------------------------"
SCHEDULER_URL="http://localhost:$SCHEDULER_PORT/metrics"
check_metric "$SCHEDULER_URL" "latq_jobs_submitted_total" "Scheduler" || ((FAILED++))
check_metric "$SCHEDULER_URL" "latq_queue_length" "Scheduler" || ((FAILED++))
check_metric "$SCHEDULER_URL" "latq_workers_registered" "Scheduler" || ((FAILED++))
check_metric "$SCHEDULER_URL" "latq_running_jobs_total" "Scheduler" || ((FAILED++))
check_metric "$SCHEDULER_URL" "latq_recovery_events_total" "Scheduler" || ((FAILED++))
echo ""

echo "👷 Validating Worker Metrics..."
echo "-----------------------------"
WORKER_URL="http://localhost:$WORKER_PORT/metrics"
check_metric "$WORKER_URL" "latq_jobs_completed_total" "Worker" || ((FAILED++))
check_metric "$WORKER_URL" "latq_job_duration_seconds" "Worker" || ((FAILED++))
check_metric "$WORKER_URL" "latq_jobs_requeued_total" "Worker" || ((FAILED++))
echo ""

echo "🎯 Checking Prometheus Targets..."
echo "--------------------------------"
if curl -s "http://localhost:$PROMETHEUS_PORT/api/v1/targets" | grep -q '"health":"up"'; then
    TARGETS=$(curl -s "http://localhost:$PROMETHEUS_PORT/api/v1/targets" | grep -o '"health":"up"' | wc -l)
    echo -e "${GREEN}✓${NC} Prometheus has $TARGETS target(s) UP"
else
    echo -e "${RED}✗${NC} No Prometheus targets are UP"
    ((FAILED++))
fi
echo ""

echo "📈 Testing Metric Queries..."
echo "--------------------------"
# Test if we can query metrics
QUERY_RESULT=$(curl -s "http://localhost:$PROMETHEUS_PORT/api/v1/query?query=latq_queue_length" | grep -o '"status":"success"')
if [ -n "$QUERY_RESULT" ]; then
    echo -e "${GREEN}✓${NC} Successfully queried latq_queue_length"
else
    echo -e "${RED}✗${NC} Failed to query metrics from Prometheus"
    ((FAILED++))
fi

QUERY_RESULT=$(curl -s "http://localhost:$PROMETHEUS_PORT/api/v1/query?query=latq_workers_registered" | grep -o '"status":"success"')
if [ -n "$QUERY_RESULT" ]; then
    echo -e "${GREEN}✓${NC} Successfully queried latq_workers_registered"
else
    echo -e "${RED}✗${NC} Failed to query worker metrics"
    ((FAILED++))
fi
echo ""

echo "🎨 Checking Grafana Setup..."
echo "--------------------------"
# Check if datasource is configured
if curl -s -u admin:admin "http://localhost:$GRAFANA_PORT/api/datasources" | grep -q "Prometheus"; then
    echo -e "${GREEN}✓${NC} Prometheus datasource configured in Grafana"
else
    echo -e "${RED}✗${NC} Prometheus datasource not found in Grafana"
    ((FAILED++))
fi

# Check if dashboard exists
if curl -s -u admin:admin "http://localhost:$GRAFANA_PORT/api/search" | grep -q "Latency-Aware Task Queue"; then
    echo -e "${GREEN}✓${NC} Dashboard 'Latency-Aware Task Queue' found"
else
    echo -e "${YELLOW}⚠${NC} Dashboard not found (may need manual import)"
fi
echo ""

echo "🔢 Current Metric Values..."
echo "-------------------------"
echo "Queue Lengths:"
curl -s "$SCHEDULER_URL" | grep "latq_queue_length{" | head -5

echo ""
echo "Workers Registered:"
curl -s "$SCHEDULER_URL" | grep "latq_workers_registered "

echo ""
echo "Jobs Completed:"
curl -s "$WORKER_URL" | grep "latq_jobs_completed_total{" | head -3
echo ""

# Summary
echo "================================"
if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✅ All checks passed!${NC}"
    echo ""
    echo "🎉 Your metrics setup is working correctly!"
    echo ""
    echo "Access points:"
    echo "  • Scheduler Metrics: http://localhost:$SCHEDULER_PORT/metrics"
    echo "  • Worker Metrics: http://localhost:$WORKER_PORT/metrics"
    echo "  • Prometheus: http://localhost:$PROMETHEUS_PORT"
    echo "  • Grafana: http://localhost:$GRAFANA_PORT (admin/admin)"
    echo ""
    exit 0
else
    echo -e "${RED}❌ $FAILED check(s) failed${NC}"
    echo ""
    echo "Troubleshooting steps:"
    echo "  1. Ensure all services are running: docker-compose ps"
    echo "  2. Check logs: docker-compose logs prometheus grafana"
    echo "  3. Verify scheduler is running: ps aux | grep scheduler"
    echo "  4. Verify worker is running: ps aux | grep worker"
    echo "  5. Check Redis: redis-cli ping"
    echo ""
    exit 1
fi