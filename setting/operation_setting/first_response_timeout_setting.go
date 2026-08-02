package operation_setting

import (
	"fmt"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	MinFirstResponseTimeoutSeconds            = 1
	MaxFirstResponseTimeoutSeconds            = 300
	MinFirstResponseDisableWindowMinutes      = 1
	MaxFirstResponseDisableWindowMinutes      = 60
	MinFirstResponseDisableMinTimeoutAttempts = 1
	MaxFirstResponseDisableMinTimeoutAttempts = 100
	MinFailureSampleMaxMB                     = 1
	MaxFailureSampleMaxMB                     = 16
)

type FirstResponseTimeoutSetting struct {
	RetryEnabled               bool    `json:"retry_enabled"`
	TimeoutSeconds             int     `json:"timeout_seconds"`
	DisableEnabled             bool    `json:"disable_enabled"`
	DisableWindowMinutes       int     `json:"disable_window_minutes"`
	DisableRate                float64 `json:"disable_rate"`
	DisableMinTimeoutAttempts  int     `json:"disable_min_timeout_attempts"`
	FailureSampleReplayEnabled bool    `json:"failure_sample_replay_enabled"`
	FailureSampleMaxMB         int     `json:"failure_sample_max_mb"`
}

var firstResponseTimeoutSetting = FirstResponseTimeoutSetting{
	RetryEnabled:               false,
	TimeoutSeconds:             20,
	DisableEnabled:             false,
	DisableWindowMinutes:       5,
	DisableRate:                30,
	DisableMinTimeoutAttempts:  2,
	FailureSampleReplayEnabled: false,
	FailureSampleMaxMB:         4,
}

func init() {
	config.GlobalConfig.Register("first_response_timeout_setting", &firstResponseTimeoutSetting)
}

func GetFirstResponseTimeoutSetting() *FirstResponseTimeoutSetting {
	return &firstResponseTimeoutSetting
}

func ValidateFirstResponseTimeoutSeconds(seconds int) error {
	if seconds < MinFirstResponseTimeoutSeconds || seconds > MaxFirstResponseTimeoutSeconds {
		return fmt.Errorf("first response timeout must be between %d and %d seconds", MinFirstResponseTimeoutSeconds, MaxFirstResponseTimeoutSeconds)
	}
	return nil
}

func ValidateFirstResponseDisableWindowMinutes(minutes int) error {
	if minutes < MinFirstResponseDisableWindowMinutes || minutes > MaxFirstResponseDisableWindowMinutes {
		return fmt.Errorf("first response disable window must be between %d and %d minutes", MinFirstResponseDisableWindowMinutes, MaxFirstResponseDisableWindowMinutes)
	}
	return nil
}

func ValidateFirstResponseDisableRate(rate float64) error {
	if rate <= 0 || rate > 100 {
		return fmt.Errorf("first response disable rate must be greater than 0 and at most 100")
	}
	return nil
}

func ValidateFirstResponseDisableMinTimeoutAttempts(attempts int) error {
	if attempts < MinFirstResponseDisableMinTimeoutAttempts || attempts > MaxFirstResponseDisableMinTimeoutAttempts {
		return fmt.Errorf("minimum first response timeout attempts must be between %d and %d", MinFirstResponseDisableMinTimeoutAttempts, MaxFirstResponseDisableMinTimeoutAttempts)
	}
	return nil
}

func ValidateFailureSampleMaxMB(sizeMB int) error {
	if sizeMB < MinFailureSampleMaxMB || sizeMB > MaxFailureSampleMaxMB {
		return fmt.Errorf("failure sample size must be between %d and %d MB", MinFailureSampleMaxMB, MaxFailureSampleMaxMB)
	}
	return nil
}

func FailureSampleMaxBytes() int64 {
	setting := GetFirstResponseTimeoutSetting()
	if ValidateFailureSampleMaxMB(setting.FailureSampleMaxMB) != nil {
		return 0
	}
	return int64(setting.FailureSampleMaxMB) << 20
}
