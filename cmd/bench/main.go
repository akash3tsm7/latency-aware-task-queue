package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/ak3tsm7/latency-aware-task-queue/internal/models"
	redisq "github.com/ak3tsm7/latency-aware-task-queue/internal/redis"
)

type benchConfig struct {
	addr        string
	queue       string
	jobs        int
	concurrency int
	timeoutMs   int
	shouldFail  bool
	payloadSize int
}

func main() {
	cfg := parseFlags()
	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.addr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis ping failed: %v", err)
	}

	log.Printf("Starting benchmark: jobs=%d queue=%s concurrency=%d timeout_ms=%d fail=%v",
		cfg.jobs, cfg.queue, cfg.concurrency, cfg.timeoutMs, cfg.shouldFail)

	jobIDs := enqueueJobs(ctx, rdb, cfg)

	start := time.Now()
	waitForDrain(ctx, rdb, jobIDs)
	duration := time.Since(start)

	log.Printf("Benchmark complete in %v", duration)
}

func parseFlags() benchConfig {
	cfg := benchConfig{}
	flag.StringVar(&cfg.addr, "addr", envOr("REDIS_ADDR", "localhost:6379"), "redis address")
	flag.StringVar(&cfg.queue, "queue", envOr("BENCH_QUEUE", "cpu"), "cpu|gpu|any")
	flag.IntVar(&cfg.jobs, "jobs", envInt("BENCH_JOBS", 100), "number of jobs")
	flag.IntVar(&cfg.concurrency, "concurrency", envInt("BENCH_CONCURRENCY", 10), "enqueue workers")
	flag.IntVar(&cfg.timeoutMs, "timeout", envInt("BENCH_TIMEOUT_MS", 3000), "job timeout ms")
	flag.BoolVar(&cfg.shouldFail, "fail", envBool("BENCH_FAIL", false), "set payload.should_fail")
	flag.IntVar(&cfg.payloadSize, "payload-bytes", envInt("BENCH_PAYLOAD_BYTES", 0), "extra payload bytes")
	flag.Parse()

	if cfg.queue != "cpu" && cfg.queue != "gpu" && cfg.queue != "any" {
		log.Fatalf("queue must be cpu|gpu|any")
	}
	return cfg
}

func enqueueJobs(ctx context.Context, rdb *redis.Client, cfg benchConfig) []string {
	jobIDs := make([]string, cfg.jobs)
	workCh := make(chan int)
	wg := sync.WaitGroup{}
	wg.Add(cfg.concurrency)

	for i := 0; i < cfg.concurrency; i++ {
		go func() {
			defer wg.Done()
			for idx := range workCh {
				id := uuid.New().String()
				jobIDs[idx] = id
				job := models.Job{
					ID:        id,
					TaskType:  "bench",
					Requires:  cfg.queue,
					Priority:  100,
					TimeoutMs: cfg.timeoutMs,
					Payload: map[string]interface{}{
						"bench":       true,
						"seq":         idx,
						"should_fail": cfg.shouldFail,
						"padding":     strings.Repeat("x", cfg.payloadSize),
					},
					Metadata: map[string]string{"source": "bench"},
					MaxRetries:   2,
					RetryBackoff: "linear",
				}
				if err := redisq.EnqueueJob(ctx, rdb, job); err != nil {
					log.Printf("enqueue failed: %v", err)
				}
			}
		}()
	}

	for i := 0; i < cfg.jobs; i++ {
		workCh <- i
	}
	close(workCh)
	wg.Wait()

	return jobIDs
}

func waitForDrain(ctx context.Context, rdb *redis.Client, jobIDs []string) {
	target := map[string]struct{}{}
	for _, id := range jobIDs {
		target[id] = struct{}{}
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			remaining, info := pendingJobs(ctx, rdb, target)
			log.Printf("Remaining %d; running=%d queued=%d dlq=%d", remaining, info.running, info.queued, info.dlq)
			if remaining == 0 {
				return
			}
		}
	}
}

type pendingInfo struct {
	running int
	queued  int
	dlq     int
}

func pendingJobs(ctx context.Context, rdb *redis.Client, target map[string]struct{}) (int, pendingInfo) {
	info := pendingInfo{}
	remaining := 0

	// check DLQ
	dlqIDs, _ := rdb.ZRange(ctx, "dlq:failed", 0, -1).Result()
	for _, id := range dlqIDs {
		if _, ok := target[id]; ok {
			delete(target, id)
			info.dlq++
		}
	}

	// check queues
	for _, q := range []string{"queue:cpu", "queue:gpu", "queue:any"} {
		ids, _ := rdb.ZRange(ctx, q, 0, -1).Result()
		for _, id := range ids {
			if _, ok := target[id]; ok {
				info.queued++
			}
		}
	}

	// check running
	keys, _ := rdb.Keys(ctx, "running:*").Result()
	for _, k := range keys {
		rids, _ := rdb.HKeys(ctx, k).Result()
		for _, id := range rids {
			if _, ok := target[id]; ok {
				info.running++
			}
		}
	}

	for id := range target {
		// if no queue/running/dlq matches, check status; missing/empty => treated as completed
		status, _ := rdb.HGet(ctx, fmt.Sprintf("job:%s", id), "status").Result()
		switch status {
		case "running", "queued", "retry_scheduled":
			remaining++
		case "":
			delete(target, id)
		default:
			delete(target, id)
		}
	}
	return remaining, info
}

// util helpers
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		var b bool
		if err := json.Unmarshal([]byte(strings.ToLower(v)), &b); err == nil {
			return b
		}
	}
	return def
}
