package redisq

import (
	"context"
	"github.com/redis/go-redis/v9"
	"time"
)

func UpdateWorkerMetrics(
	ctx context.Context,
	rdb *redis.Client,
	workerID string,
	workerType string,
	executionTime time.Duration,
) error {
	return nil
}
