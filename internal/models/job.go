package models

import "time"

type Job struct {
	ID        string                 `json:"job_id"`
	TaskType  string                 `json:"task_type"`
	Requires  string                 `json:"requires"` // cpu | gpu | any
	Priority  int                    `json:"priority"`
	Payload   map[string]interface{} `json:"payload"`
	TimeoutMs int                    `json:"timeout_ms"`
	Metadata  map[string]string      `json:"metadata"`

	RetryCount   int       `json:"retry_count"`
	MaxRetries   int       `json:"max_retries"`
	RetryBackoff string    `json:"retry_backoff"` // "exponential" or "linear"
	FailedAt     time.Time `json:"failed_at"`
	ErrorMessage string    `json:"error_message"`
}
