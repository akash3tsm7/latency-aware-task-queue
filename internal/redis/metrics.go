package redisq

// Add this import at the top of internal/redis/metrics.go after line 9

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ak3tsm7/latency-aware-task-queue/internal/models"  // ADD THIS LINE
)

// The rest of the file remains the same

const emaAlpha = 0.2 // Exponential moving average smoothing factor

func UpdateWorkerMetrics(
	ctx context.Context,
	rdb *redis.Client,
	workerID string,
	workerType string,
	executionTime time.Duration,
) error {

	key := "metrics:" + workerID
	currentMs := float64(executionTime.Milliseconds())

	// Get existing average latency
	existingAvg, err := rdb.HGet(ctx, key, "avg_latency_ms").Result()
	var newAvg float64

	if err == redis.Nil {
		// First job → initialize with current execution time
		newAvg = currentMs
	} else if err != nil {
		return fmt.Errorf("failed to get existing metrics: %w", err)
	} else {
		// Calculate exponential moving average
		oldAvg, parseErr := strconv.ParseFloat(existingAvg, 64)
		if parseErr != nil {
			// If parse fails, reset to current value
			newAvg = currentMs
		} else {
			newAvg = emaAlpha*currentMs + (1-emaAlpha)*oldAvg
		}
	}

	// Use pipeline for atomic updates
	pipe := rdb.TxPipeline()

	// Update worker metrics
	pipe.HSet(ctx, key,
		"avg_latency_ms", fmt.Sprintf("%.2f", newAvg),
		"worker_type", workerType,
		"last_updated", time.Now().Unix(),
	)
	
	// Increment jobs done counter
	pipe.HIncrBy(ctx, key, "jobs_done", 1)

	// Update worker ranking in sorted set (lower latency = better = lower score)
	pipe.ZAdd(ctx, "workers:latency", redis.Z{
		Score:  newAvg,
		Member: workerID,
	})

	// Execute pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update metrics: %w", err)
	}

	return nil
}

func GetWorkerMetrics(
	ctx context.Context,
	rdb *redis.Client,
	workerID string,
) (*models.WorkerMetrics, error) {
	
	key := "metrics:" + workerID
	data, err := rdb.HGetAll(ctx, key).Result()
	
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}
	
	if len(data) == 0 {
		return nil, fmt.Errorf("no metrics found for worker %s", workerID)
	}

	avgLatency, _ := strconv.ParseFloat(data["avg_latency_ms"], 64)
	jobsDone, _ := strconv.ParseInt(data["jobs_done"], 10, 64)

	return &models.WorkerMetrics{
		WorkerID:     workerID,
		WorkerType:   data["worker_type"],
		AvgLatencyMs: avgLatency,
		JobsDone:     jobsDone,
	}, nil
}

func GetTopWorkers(
	ctx context.Context,
	rdb *redis.Client,
	limit int64,
) ([]models.WorkerMetrics, error) {
	
	// Get workers sorted by latency (ascending - best workers first)
	workers, err := rdb.ZRangeWithScores(ctx, "workers:latency", 0, limit-1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get top workers: %w", err)
	}

	result := make([]models.WorkerMetrics, 0, len(workers))
	
	for _, w := range workers {
		workerID := w.Member.(string)
		metrics, err := GetWorkerMetrics(ctx, rdb, workerID)
		
		if err == nil {
			result = append(result, *metrics)
		}
	}

	return result, nil
}