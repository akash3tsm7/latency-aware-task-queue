package main

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ak3tsm7/latency-aware-task-queue/internal/models"
	redisq "github.com/ak3tsm7/latency-aware-task-queue/internal/redis"
)

func main() {
	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// Test Redis connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Println("Failed to connect to Redis:", err)
		return
	}

	// Create worker (change "gpu" to "cpu" for CPU worker)
	worker := models.NewWorker("cpu")

	// 1️⃣ Self-register
	fmt.Printf("🚀 Worker %s (%s) started\n", worker.ID, worker.Type)
	fmt.Println("Registering with scheduler...")
	
	err := rdb.ZAdd(ctx, "workers:latency", redis.Z{
		Score:  1000, // Start with high latency (will improve with actual jobs)
		Member: worker.ID,
	}).Err()
	
	if err != nil {
		fmt.Println("Failed to register:", err)
		return
	}
	
	fmt.Println("✓ Registration successful")

	// 2️⃣ Polling Loop
	fmt.Println("Waiting for jobs...\n")
	
	for {
		job, err := redisq.FetchAndClaimJob(ctx, rdb, worker)
		if err != nil {
			fmt.Println("❌ Error fetching job:", err)
			time.Sleep(1 * time.Second)
			continue
		}

		if job == nil {
			// No job available - wait and retry
			time.Sleep(1 * time.Second)
			continue
		}

		fmt.Printf("\n📋 Worker %s received job %s\n", worker.ID, job.ID)
		fmt.Printf("   Task: %s | Type: %s | Priority: %d\n", 
			job.TaskType, job.Requires, job.Priority)

		heartbeatKey := fmt.Sprintf("heartbeat:%s", worker.ID)
		runningKey := fmt.Sprintf("running:%s", worker.ID)

		// 3️⃣ Start heartbeat goroutine
		stopHB := make(chan struct{})
		go func() {
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					err := rdb.Set(ctx, heartbeatKey, time.Now().Unix(), 15*time.Second).Err()
					if err != nil {
						fmt.Println("⚠️  Heartbeat failed:", err)
					}
				case <-stopHB:
					return
				}
			}
		}()

		// 4️⃣ Execute job
		start := time.Now()
		
		// Simulate work (in production, this would be actual task execution)
		executionTime := time.Duration(2+job.Priority%3) * time.Second
		fmt.Printf("   ⏳ Executing for %v...\n", executionTime)
		time.Sleep(executionTime)
		
		duration := time.Since(start)
		fmt.Printf("   ✓ Completed in %v\n", duration)

		// 5️⃣ Update metrics
		err = redisq.UpdateWorkerMetrics(
			ctx,
			rdb,
			worker.ID,
			worker.Type,
			duration,
		)
		if err != nil {
			fmt.Println("⚠️  Failed to update metrics:", err)
		}

		// 6️⃣ Stop heartbeat
		close(stopHB)

		// 7️⃣ Cleanup
		rdb.HDel(ctx, runningKey, job.ID)
		rdb.Del(ctx, heartbeatKey)

		// 8️⃣ Delete job data (optional - keep for audit trail in production)
		jobKey := fmt.Sprintf("job:%s", job.ID)
		rdb.Del(ctx, jobKey)

		// Display updated metrics
		metrics, _ := rdb.HGetAll(ctx, "metrics:"+worker.ID).Result()
		if len(metrics) > 0 {
			fmt.Printf("   📊 Updated Metrics - Avg Latency: %sms | Jobs Done: %s\n", 
				metrics["avg_latency_ms"], metrics["jobs_done"])
		}
	}
}