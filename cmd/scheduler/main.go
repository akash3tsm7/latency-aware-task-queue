package main

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/google/uuid"

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
	fmt.Println("Connected to Redis successfully")

	// 1️⃣ Run recovery FIRST to handle any stuck jobs
	fmt.Println("Running recovery scan...")
	recoverStuckJobs(ctx, rdb)

	// 2️⃣ Enqueue test jobs
	jobs := []models.Job{
		{
			ID:        uuid.New().String(),
			TaskType:  "image_processing",
			Requires:  "gpu",
			Priority:  100,
			Payload:   map[string]interface{}{"image": "test1.jpg", "operation": "resize"},
			TimeoutMs: 5000,
			Metadata:  map[string]string{"source": "api"},
		},
		{
			ID:        uuid.New().String(),
			TaskType:  "data_processing",
			Requires:  "cpu",
			Priority:  80,
			Payload:   map[string]interface{}{"dataset": "sales_data.csv"},
			TimeoutMs: 3000,
			Metadata:  map[string]string{"source": "batch"},
		},
		{
			ID:        uuid.New().String(),
			TaskType:  "general_task",
			Requires:  "any",
			Priority:  50,
			Payload:   map[string]interface{}{"task": "cleanup"},
			TimeoutMs: 2000,
			Metadata:  map[string]string{"source": "cron"},
		},
	}

	for _, job := range jobs {
		err := redisq.EnqueueJob(ctx, rdb, job)
		if err != nil {
			fmt.Printf("Failed to enqueue job %s: %v\n", job.ID, err)
		} else {
			fmt.Printf("✓ Job %s enqueued to queue:%s (priority: %d)\n", 
				job.ID, job.Requires, job.Priority)
		}
	}

	// 3️⃣ Display current worker rankings
	fmt.Println("\n📊 Current Worker Rankings (by latency):")
	workers, err := rdb.ZRangeWithScores(ctx, "workers:latency", 0, -1).Result()
	if err == nil && len(workers) > 0 {
		for i, w := range workers {
			fmt.Printf("  %d. Worker %s - Avg Latency: %.2fms\n", 
				i+1, w.Member, w.Score)
		}
	} else {
		fmt.Println("  No workers registered yet")
	}

	// 4️⃣ Show queue status
	fmt.Println("\n📦 Queue Status:")
	queues := []string{"queue:gpu", "queue:cpu", "queue:any"}
	for _, queue := range queues {
		count, _ := rdb.ZCard(ctx, queue).Result()
		fmt.Printf("  %s: %d jobs\n", queue, count)
	}

	fmt.Println("\n✓ Scheduler completed. Workers can now process these jobs.")
}