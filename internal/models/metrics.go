package models

type WorkerMetrics struct {
	WorkerID     string
	WorkerType   string  // cpu / gpu
	AvgLatencyMs float64 // exponential moving average
	JobsDone     int64
}
