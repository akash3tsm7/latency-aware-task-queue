package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/ak3tsm7/latency-aware-task-queue/internal/metrics"
	"github.com/ak3tsm7/latency-aware-task-queue/internal/models"
	redisq "github.com/ak3tsm7/latency-aware-task-queue/internal/redis"
)

func main() {
	ctx := context.Background()

	// Initialize metrics
	metrics.Register()

	// Start Prometheus metrics server
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		fmt.Println("ðŸ“Š Metrics server started on :2113/metrics")
		if err := http.ListenAndServe(":2113", nil); err != nil {
			fmt.Printf("Failed to start metrics server: %v\n", err)
		}
	}()

	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// Test Redis connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Println("Failed to connect to Redis:", err)
		return
	}

	// Create worker (change "cpu" to "gpu" for GPU worker)
	worker := models.NewWorker("cpu")

	// 1ï¸âƒ£ Self-register
	fmt.Printf("ðŸš€ Worker %s (%s) started\n", worker.ID, worker.Type)
	fmt.Println("Registering with scheduler...")

	err := rdb.ZAdd(ctx, "workers:latency", redis.Z{
		Score:  1000, // Start with high latency (will improve with actual jobs)
		Member: worker.ID,
	}).Err()

	if err != nil {
		fmt.Println("Failed to register:", err)
		return
	}

	fmt.Println("âœ… Registration successful")

	// 2ï¸âƒ£ Polling Loop
	fmt.Println("Waiting for jobs...")

	for {
		job, err := redisq.FetchAndClaimJob(ctx, rdb, worker)
		if err != nil {
			fmt.Println("âŒ Error fetching job:", err)
			time.Sleep(1 * time.Second)
			continue
		}

		if job == nil {
			// No job available - wait and retry
			time.Sleep(1 * time.Second)
			continue
		}

		fmt.Printf("\nðŸ“‹ Worker %s received job %s\n", worker.ID, job.ID)
		fmt.Printf("   Task: %s | Type: %s | Priority: %d\n",
			job.TaskType, job.Requires, job.Priority)

		heartbeatKey := fmt.Sprintf("heartbeat:%s", worker.ID)
		runningKey := fmt.Sprintf("running:%s", worker.ID)

		// 3ï¸âƒ£ Start heartbeat goroutine
		stopHB := make(chan struct{})
		go func() {
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					err := rdb.Set(ctx, heartbeatKey, time.Now().Unix(), 15*time.Second).Err()
					if err != nil {
						fmt.Println("âš ï¸  Heartbeat failed:", err)
					}
				case <-stopHB:
					return
				}
			}
		}()

		// 4ï¸âƒ£ Execute job
		start := time.Now()
		var execErr error

		// Simulate work (in production, this would be actual task execution)
		executionTime := time.Duration(2+job.Priority%3) * time.Second
		fmt.Printf("   â³ Executing for %v...\n", executionTime)
		time.Sleep(executionTime)

		// Optional demo failure: set payload.should_fail=true to trigger retries/DLQ.
		if v, ok := job.Payload["should_fail"].(bool); ok && v {
			execErr = errors.New("simulated job failure (payload.should_fail=true)")
		}

		duration := time.Since(start)
		if execErr == nil {
			fmt.Printf("   âœ… Completed in %v\n", duration)
		} else {
			fmt.Printf("   âŒ Failed in %v: %v\n", duration, execErr)
		}

		// Record job duration in Prometheus histogram
		metrics.JobDurationSeconds.WithLabelValues(job.Requires, worker.Type).Observe(duration.Seconds())

		// Increment completion counter
		successLabel := "true"
		if execErr != nil {
			successLabel = "false"
		}
		metrics.JobsCompletedTotal.WithLabelValues(job.Requires, worker.Type, successLabel).Inc()

		// 5ï¸âƒ£ Update metrics
		err = redisq.UpdateWorkerMetrics(
			ctx,
			rdb,
			worker.ID,
			worker.Type,
			duration,
		)
		if err != nil {
			fmt.Println("âš ï¸  Failed to update metrics:", err)
		}

		// 6ï¸âƒ£ Stop heartbeat
		close(stopHB)

		// 7ï¸âƒ£ Cleanup
		rdb.HDel(ctx, runningKey, job.ID)
		rdb.Del(ctx, heartbeatKey)

		jobKey := fmt.Sprintf("job:%s", job.ID)
		if execErr != nil {
			// Schedule retry or move to DLQ (job data is intentionally retained).
			if err := redisq.HandleJobFailure(ctx, rdb, *job, execErr); err != nil {
				fmt.Println("âš ï¸  Failed to record job failure:", err)
			}
		} else {
			// Delete job data (optional - keep for audit trail in production)
			rdb.Del(ctx, jobKey)
		}

		// Display updated metrics
		metricsData, _ := rdb.HGetAll(ctx, "metrics:"+worker.ID).Result()
		if len(metricsData) > 0 {
			fmt.Printf("   ðŸ“Š Updated Metrics - Avg Latency: %sms | Jobs Done: %s\n",
				metricsData["avg_latency_ms"], metricsData["jobs_done"])
		}
	}
}
