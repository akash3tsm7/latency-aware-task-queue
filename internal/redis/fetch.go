package redisq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ak3tsm7/latency-aware-task-queue/internal/models"
)

func FetchAndClaimJob(
	ctx context.Context,
	rdb *redis.Client,
	worker models.Worker,
) (*models.Job, error) {

	// Define queue priority order for this worker
	// 1. Worker-type specific queue (cpu/gpu)
	// 2. Generic "any" queue
	queues := []string{
		fmt.Sprintf("queue:%s", worker.Type),
		"queue:any",
	}

	var jobID string
	var foundQueue string

	// Try each queue in priority order
	for _, queueKey := range queues {
		// Atomically pop highest priority job (highest score = least negative)
		zres, err := rdb.ZPopMax(ctx, queueKey, 1).Result()
		
		if err != nil && err != redis.Nil {
			return nil, fmt.Errorf("failed to pop from queue %s: %w", queueKey, err)
		}
		
		if len(zres) > 0 {
			jobID = zres[0].Member.(string)
			foundQueue = queueKey
			break
		}
	}

	// No job found in any queue
	if jobID == "" {
		return nil, nil
	}

	// Fetch job payload
	jobKey := fmt.Sprintf("job:%s", jobID)
	raw, err := rdb.HGet(ctx, jobKey, "payload").Result()

	if err == redis.Nil || raw == "" {
		_ = MoveToDLQInvalidPayload(ctx, rdb, jobID, raw, fmt.Errorf("missing payload"))
		return nil, fmt.Errorf("job %s payload missing", jobID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch job data: %w", err)
	}

	var job models.Job
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		// Payload is corrupted/invalid JSON. Move to DLQ so workers don't loop forever.
		_ = MoveToDLQInvalidPayload(ctx, rdb, jobID, raw, err)
		return nil, fmt.Errorf("failed to unmarshal job: %w", err)
	}

	// Mark as running
	runningKey := fmt.Sprintf("running:%s", worker.ID)
	err = rdb.HSet(ctx, runningKey, job.ID, time.Now().Unix()).Err()
	
	if err != nil {
		// If we can't mark it as running, requeue it
		rdb.ZAdd(ctx, foundQueue, redis.Z{
			Score:  float64(-job.Priority),
			Member: job.ID,
		})
		return nil, fmt.Errorf("failed to mark job as running: %w", err)
	}

	// Update job status
	rdb.HSet(ctx, jobKey, "status", "running")

	return &job, nil
}
