package redisq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ak3tsm7/latency-aware-task-queue/internal/models"
)

const (
	retryScheduledKey = "retry:scheduled" // zset: score=available_at_unix, member=job_id
	dlqFailedKey      = "dlq:failed"      // zset: score=failed_at_unix, member=job_id
)

func defaultMaxRetries(job models.Job) int {
	if job.MaxRetries > 0 {
		return job.MaxRetries
	}
	return 3
}

func backoffDuration(job models.Job) time.Duration {
	// Base delay kept intentionally small for this demo project.
	const base = 1 * time.Second
	const max = 1 * time.Minute

	attempt := job.RetryCount
	if attempt < 1 {
		attempt = 1
	}

	switch strings.ToLower(job.RetryBackoff) {
	case "linear":
		d := time.Duration(attempt) * base
		if d > max {
			return max
		}
		return d
	case "exponential", "":
		factor := math.Pow(2, float64(attempt-1))
		d := time.Duration(factor) * base
		if d > max {
			return max
		}
		return d
	default:
		// Unknown strategy: fall back to exponential.
		factor := math.Pow(2, float64(attempt-1))
		d := time.Duration(factor) * base
		if d > max {
			return max
		}
		return d
	}
}

// HandleJobFailure increments retry state and either schedules a retry or moves the job to DLQ.
//
// Redis keys used:
// - retry:scheduled (zset)              score=available_at_unix, member=job_id
// - dlq:failed (sorted set)             score=failed_at_unix, member=job_id
// - dlq:job:<id> (hash)                 failure metadata
// - job:<id> (hash, existing)           stores payload + status
func HandleJobFailure(ctx context.Context, rdb *redis.Client, job models.Job, jobErr error) error {
	if jobErr == nil {
		jobErr = errors.New("job failed")
	}

	now := time.Now()
	job.RetryCount++
	job.FailedAt = now
	job.ErrorMessage = jobErr.Error()

	jobKey := fmt.Sprintf("job:%s", job.ID)

	payloadJSON, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job for retry update: %w", err)
	}

	maxRetries := defaultMaxRetries(job)
	if job.RetryCount > maxRetries {
		// Move to DLQ
		dlqJobKey := fmt.Sprintf("dlq:job:%s", job.ID)
		pipe := rdb.Pipeline()

		pipe.HSet(ctx, jobKey, map[string]interface{}{
			"payload":    string(payloadJSON),
			"status":     "failed",
			"failed_at":  now.Unix(),
			"error":      job.ErrorMessage,
			"retry_count": job.RetryCount,
			"max_retries": maxRetries,
		})

		pipe.ZAdd(ctx, dlqFailedKey, redis.Z{Score: float64(now.Unix()), Member: job.ID})
		pipe.HSet(ctx, dlqJobKey, map[string]interface{}{
			"job_id":       job.ID,
			"task_type":    job.TaskType,
			"requires":     job.Requires,
			"priority":     job.Priority,
			"retry_count":  job.RetryCount,
			"max_retries":  maxRetries,
			"retry_backoff": job.RetryBackoff,
			"failed_at":    now.Unix(),
			"error_message": job.ErrorMessage,
		})

		pipe.ZRem(ctx, retryScheduledKey, job.ID)
		_, err := pipe.Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to write DLQ entries: %w", err)
		}
		return nil
	}

	// Schedule retry
	delay := backoffDuration(job)
	availableAt := now.Add(delay).Unix()

	pipe := rdb.Pipeline()
	pipe.HSet(ctx, jobKey, map[string]interface{}{
		"payload":      string(payloadJSON),
		"status":       "retry_scheduled",
		"failed_at":    now.Unix(),
		"error":        job.ErrorMessage,
		"retry_count":  job.RetryCount,
		"max_retries":  maxRetries,
		"available_at": availableAt,
	})
	pipe.ZAdd(ctx, retryScheduledKey, redis.Z{Score: float64(availableAt), Member: job.ID})
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to schedule retry: %w", err)
	}
	return nil
}

// PromoteDueRetries moves jobs from retry:scheduled into their normal priority queues once their backoff expires.
func PromoteDueRetries(ctx context.Context, rdb *redis.Client, limit int64) (int, error) {
	if limit <= 0 {
		limit = 100
	}

	now := time.Now().Unix()
	due, err := rdb.ZRangeByScore(ctx, retryScheduledKey, &redis.ZRangeBy{
		Min:    "-inf",
		Max:    fmt.Sprintf("%d", now),
		Offset: 0,
		Count:  limit,
	}).Result()
	if err != nil && err != redis.Nil {
		return 0, fmt.Errorf("failed to fetch due retries: %w", err)
	}
	if len(due) == 0 {
		return 0, nil
	}

	promoted := 0
	for _, jobID := range due {
		// Best-effort: remove first to avoid repeat promotion on crashy loops.
		removed, err := rdb.ZRem(ctx, retryScheduledKey, jobID).Result()
		if err != nil {
			return promoted, fmt.Errorf("failed to remove due retry %s: %w", jobID, err)
		}
		if removed == 0 {
			continue
		}

		jobKey := fmt.Sprintf("job:%s", jobID)
		raw, err := rdb.HGet(ctx, jobKey, "payload").Result()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return promoted, fmt.Errorf("failed to read job payload for %s: %w", jobID, err)
		}

		var job models.Job
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			return promoted, fmt.Errorf("failed to unmarshal job %s: %w", jobID, err)
		}

		queue := "queue:any"
		if job.Requires == "cpu" || job.Requires == "gpu" {
			queue = fmt.Sprintf("queue:%s", job.Requires)
		}

		pipe := rdb.Pipeline()
		pipe.ZAdd(ctx, queue, redis.Z{Score: float64(-job.Priority), Member: job.ID})
		pipe.HSet(ctx, jobKey, "status", "queued")
		if _, err := pipe.Exec(ctx); err != nil {
			return promoted, fmt.Errorf("failed to promote retry job %s: %w", jobID, err)
		}

		promoted++
	}

	return promoted, nil
}

// MoveToDLQInvalidPayload moves a job to DLQ when its stored payload is not valid JSON.
// This prevents workers from getting stuck in a pop->unmarshal error loop.
func MoveToDLQInvalidPayload(ctx context.Context, rdb *redis.Client, jobID string, rawPayload string, parseErr error) error {
	now := time.Now()

	errMsg := "invalid job payload"
	if parseErr != nil {
		errMsg = fmt.Sprintf("invalid job payload: %v", parseErr)
	}

	jobKey := fmt.Sprintf("job:%s", jobID)
	dlqJobKey := fmt.Sprintf("dlq:job:%s", jobID)

	pipe := rdb.Pipeline()
	pipe.HSet(ctx, jobKey, map[string]interface{}{
		// Keep existing payload as-is; also record failure markers for debugging.
		"payload":    rawPayload,
		"status":     "invalid_payload",
		"failed_at":  now.Unix(),
		"error":      errMsg,
	})
	pipe.ZAdd(ctx, dlqFailedKey, redis.Z{Score: float64(now.Unix()), Member: jobID})
	pipe.HSet(ctx, dlqJobKey, map[string]interface{}{
		"job_id":        jobID,
		"failed_at":     now.Unix(),
		"error_message": errMsg,
	})
	pipe.ZRem(ctx, retryScheduledKey, jobID)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to move invalid payload job to DLQ: %w", err)
	}
	return nil
}
