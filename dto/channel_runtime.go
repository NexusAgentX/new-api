package dto

// FirstResponseTimeoutStats contains rolling, non-persistent channel metrics.
type FirstResponseTimeoutStats struct {
	TotalAttempts   int64   `json:"total_attempts"`
	TimeoutAttempts int64   `json:"timeout_attempts"`
	TimeoutRate     float64 `json:"timeout_rate"`
	WindowMinutes   int     `json:"window_minutes"`
}
