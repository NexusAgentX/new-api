package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type RawExchangeSettings struct {
	Enabled             bool  `json:"enabled"`
	MaxRequestBytes     int64 `json:"max_request_bytes"`
	MaxResponseBytes    int64 `json:"max_response_bytes"`
	MaxExchangeBytes    int64 `json:"max_exchange_bytes"`
	GlobalCapacityBytes int64 `json:"global_capacity_bytes"`
	RetentionDays       int   `json:"retention_days"`
	PendingTTLMinutes   int   `json:"pending_ttl_minutes"`
	GCIntervalMinutes   int   `json:"gc_interval_minutes"`
}

var defaultRawExchangeSettings = RawExchangeSettings{
	Enabled:             false,
	MaxRequestBytes:     16 << 20,
	MaxResponseBytes:    32 << 20,
	MaxExchangeBytes:    48 << 20,
	GlobalCapacityBytes: 10 << 30,
	RetentionDays:       7,
	PendingTTLMinutes:   60,
	GCIntervalMinutes:   15,
}

func init() {
	config.GlobalConfig.Register("raw_exchange", &defaultRawExchangeSettings)
}

func GetRawExchangeSettings() *RawExchangeSettings {
	return &defaultRawExchangeSettings
}
