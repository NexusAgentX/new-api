package operation_setting

import (
	"fmt"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	MinFirstResponseTimeoutSeconds       = 1
	MaxFirstResponseTimeoutSeconds       = 300
	MinFirstResponseDisableWindowMinutes = 1
	MaxFirstResponseDisableWindowMinutes = 60
)

type FirstResponseTimeoutSetting struct {
	RetryEnabled         bool    `json:"retry_enabled"`
	TimeoutSeconds       int     `json:"timeout_seconds"`
	DisableEnabled       bool    `json:"disable_enabled"`
	DisableWindowMinutes int     `json:"disable_window_minutes"`
	DisableRate          float64 `json:"disable_rate"`
}

var firstResponseTimeoutSetting = FirstResponseTimeoutSetting{
	RetryEnabled:         false,
	TimeoutSeconds:       20,
	DisableEnabled:       false,
	DisableWindowMinutes: 5,
	DisableRate:          30,
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
