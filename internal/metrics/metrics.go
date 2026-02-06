package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Counters
	JobsSubmittedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "latq_jobs_submitted_total",
			Help: "Total number of jobs submitted to queues",
		},
		[]string{"queue_type"}, // cpu, gpu, any
	)

	JobsCompletedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "latq_jobs_completed_total",
			Help: "Total number of jobs completed by workers",
		},
		[]string{"queue_type", "worker_type", "success"}, // success: "true" or "false"
	)

	JobsSpeculativeTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "latq_jobs_speculative_total",
			Help: "Total number of speculative executions triggered",
		},
	)

	JobsRequeuedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "latq_jobs_requeued_total",
			Help: "Total number of jobs requeued (failures or timeouts)",
		},
	)

	RecoveryEventsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "latq_recovery_events_total",
			Help: "Total number of recovery events (stuck job recoveries)",
		},
	)

	// Gauges
	QueueLength = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "latq_queue_length",
			Help: "Current number of jobs in each queue",
		},
		[]string{"queue_type"}, // cpu, gpu, any
	)

	WorkersRegistered = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "latq_workers_registered",
			Help: "Current number of registered workers",
		},
	)

	RunningJobsTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "latq_running_jobs_total",
			Help: "Current number of jobs being executed",
		},
	)

	// Histogram for job execution duration
	// Buckets: 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.28s, 2.56s, 5.12s, 10.24s, 20.48s, 40.96s, 81.92s, 163.84s
	JobDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "latq_job_duration_seconds",
			Help:    "Job execution duration in seconds",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 15), // 10ms to ~163s
		},
		[]string{"queue_type", "worker_type"}, // cpu, gpu, any
	)
)

// Register initializes all metrics (already done via promauto, but keep for explicit initialization)
func Register() {
	// Metrics are auto-registered via promauto
	// This function exists for explicit initialization if needed
}