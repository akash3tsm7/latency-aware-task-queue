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

	queueKey := fmt.Sprintf("queue:%s", worker.Type)

	// Atomically pop highest priority job
	zres, err := rdb.ZPopMax(ctx, queueKey, 1).Result()
	if err != nil || len(zres) == 0 {
		return nil, nil
	}

	jobID := zres[0].Member.(string)
	jobKey := fmt.Sprintf("job:%s", jobID)

	// Fetch job payload
	raw, err := rdb.HGet(ctx, jobKey, "payload").Result()
	if err != nil {
		return nil, err
	}

	var job models.Job
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		return nil, err
	}

	// Mark as running
	runningKey := fmt.Sprintf("running:%s", worker.ID)
	if err := rdb.HSet(
		ctx,
		runningKey,
		job.ID,
		time.Now().Unix(),
	).Err(); err != nil {
		return nil, err
	}

	return &job, nil
}
