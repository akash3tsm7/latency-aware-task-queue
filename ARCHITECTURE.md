Architecture Overview
=====================

```mermaid
flowchart LR
    Client[Job submitter] -->|HTTP/SDK| Scheduler
    Scheduler -->|enqueue job (job:<id> + queue:*)| Redis[(Redis broker)]

    subgraph Broker
      Redis -->|retry:scheduled| Redis
      Redis -->|dlq:failed / dlq:job:*| Redis
    end

    Redis -->|claim| WorkerCPU[Worker (cpu)]
    Redis -->|claim| WorkerGPU[Worker (gpu)]
    Redis -->|claim| WorkerAny[Worker (any)]

    WorkerCPU -->|running:* + heartbeat| Redis
    WorkerGPU -->|running:* + heartbeat| Redis
    WorkerAny -->|running:* + heartbeat| Redis

    WorkerCPU -->|success| Redis
    WorkerGPU -->|success| Redis
    WorkerAny -->|success| Redis

    WorkerCPU -->|fail -> retry:scheduled| Redis
    WorkerGPU -->|fail -> retry:scheduled| Redis
    WorkerAny -->|fail -> retry:scheduled| Redis

    Scheduler -->|promote due retries| Redis
    Scheduler -->|recover stuck jobs| Redis
    API[/DELETE /api/jobs/{id}/cancel/] --> Scheduler
    Scheduler -->|set cancelled:<id>| Redis
```

Flow (happy path)
1) Client submits job → Scheduler writes `job:<id>` hash and enqueues to `queue:cpu|gpu|any`.
2) Worker claims from its queue (priority via ZSET score), marks `running:<worker>` and heartbeats.
3) Worker executes with timeout + cancellation checks.
4) On success: job hash removed (or marked done for benchmarks).

Retries & DLQ
1) On failure, worker calls `HandleJobFailure` → increments `retry_count`, either:
   - schedules into `retry:scheduled` (with backoff), or
   - moves to `dlq:failed` and `dlq:job:<id>` after exceeding `max_retries`.
2) Scheduler loop promotes due entries from `retry:scheduled` back into `queue:*`.

Cancellation
- API `DELETE /api/jobs/{id}/cancel` sets `cancelled:<id>` and marks job status=cancelled/error.
- Worker checks `cancelled:<id>` before and after execution and marks job cancelled without retry.

Latency Awareness & Rate Limits
- Workers report durations to Redis + Prometheus; `workers:latency` ZSET tracks average latency per worker.
- Fetch prefers queue matching worker type; priority ordering uses negative scores for ZSET max-pop.
- Rate limiter (`ratelimit:<queue>:<window>` and `ratelimit:worker:<id>:<window>`) throttles enqueue/execute per minute.

Speculative Execution (planned hook)
- Metric placeholder exists; a future hook can enqueue duplicate of a straggling job to another worker when `running:*` exceeds a threshold. (Not yet implemented.)
