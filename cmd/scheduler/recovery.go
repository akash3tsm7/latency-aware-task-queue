package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	heartbeatTimeoutSeconds = 10
)

func recoverStuckJobs(ctx context.Context, rdb *redis.Client) {
	// Find all running job trackers
	keys, err := rdb.Keys(ctx, "running:*").Result()
	if err != nil {
		fmt.Println("Error scanning for stuck jobs:", err)
		return
	}

	if len(keys) == 0 {
		fmt.Println("No running jobs found - nothing to recover")
		return
	}

	recoveredCount := 0
	currentTime := time.Now().Unix()

	for _, key := range keys {
		workerID := strings.TrimPrefix(key, "running:")
		hbKey := "heartbeat:" + workerID

		// Check worker heartbeat
		lastHB, err := rdb.Get(ctx, hbKey).Int64()
		isStale := false

		if err == redis.Nil {
			// No heartbeat exists - worker likely crashed
			isStale = true
			fmt.Printf("⚠️  Worker %s has no heartbeat\n", workerID)
		} else if err != nil {
			fmt.Printf("Error reading heartbeat for %s: %v\n", workerID, err)
			continue
		} else if currentTime-lastHB > heartbeatTimeoutSeconds {
			// Heartbeat is too old
			isStale = true
			fmt.Printf("⚠️  Worker %s heartbeat stale (%d seconds old)\n", 
				workerID, currentTime-lastHB)
		}

		if isStale {
			// Recover all jobs from this worker
			jobs, err := rdb.HGetAll(ctx, key).Result()
			if err != nil {
				fmt.Printf("Error getting jobs from %s: %v\n", key, err)
				continue
			}

			for jobID := range jobs {
				fmt.Printf("  🔄 Recovering job %s\n", jobID)

				// Get job details to determine correct queue
				jobKey := "job:" + jobID
				targetQueue := "queue:any" // Default fallback
				
				_, err := rdb.HGet(ctx, jobKey, "payload").Result()
				if err == nil {
					// Job data exists, requeue to "any" as fallback
					targetQueue = "queue:any"
				}

				// Requeue with original priority (default 100 if unknown)
				err = rdb.ZAdd(ctx, targetQueue, redis.Z{
					Score:  -100, // Negative for high priority in max-heap
					Member: jobID,
				}).Err()

				if err != nil {
					fmt.Printf("  ❌ Failed to requeue job %s: %v\n", jobID, err)
				} else {
					recoveredCount++
				}
			}

			// Cleanup stale worker data
			rdb.Del(ctx, key)
			rdb.Del(ctx, hbKey)
			rdb.ZRem(ctx, "workers:latency", workerID)
			
			fmt.Printf("✓ Cleaned up worker %s\n", workerID)
		}
	}

	if recoveredCount > 0 {
		fmt.Printf("✓ Recovery complete: %d jobs recovered\n", recoveredCount)
	} else {
		fmt.Println("✓ No stuck jobs found")
	}
}