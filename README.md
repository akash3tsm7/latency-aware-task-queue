# latency-aware-task-queue
atency-aware distributed task queue with priority scheduling, heterogeneous workers (CPU/GPU-modeled), and speculative execution for straggler mitigation.

## Retries & Dead Letter Queue (DLQ)

Jobs now support bounded retries with backoff and a Redis-backed DLQ.

### Job fields
`internal/models/job.go` adds:
- `retry_count`, `max_retries`
- `retry_backoff`: `"exponential"` (default) or `"linear"`
- `failed_at`, `error_message`

If `max_retries` is `0`, the system defaults to `3`.

### Redis keys
- `retry:scheduled` (sorted set): retry jobs waiting for backoff (score = unix `available_at`).
- `dlq:failed` (sorted set): jobs that exceeded max retries (score = unix `failed_at`).
- `dlq:job:<id>` (hash): failure metadata for a DLQ'd job.

### Workflow
- Worker failure calls `HandleJobFailure(...)` to either:
  - schedule a retry into `retry:scheduled`, or
  - move the job into `dlq:failed` and write `dlq:job:<id>`.
- Scheduler runs `PromoteDueRetries(...)` and moves due jobs back into `queue:*` for workers to pick up.
