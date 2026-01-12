package main

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/ak3tsm7/latency-aware-task-queue/internal/models"
	redisq "github.com/ak3tsm7/latency-aware-task-queue/internal/redis"
)

func main() {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	worker := models.NewWorker("gpu")

	job, err := redisq.FetchAndClaimJob(context.Background(), rdb, worker)
	if err != nil {
		panic(err)
	}

	if job == nil {
		fmt.Println("No job available")
		return
	}

	fmt.Printf("Worker %s claimed job %s\n", worker.ID, job.ID)
}
