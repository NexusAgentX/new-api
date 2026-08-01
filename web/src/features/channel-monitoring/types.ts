/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
export type ChannelMonitoringSearch = {
  hours: number
  channel?: number
  detail?: number
  group?: string
  model?: string
  endpoint?: string
}

export type ChannelMonitoringQueryParams = {
  hours: number
  channel_id?: number
  group?: string
  model?: string
  endpoint?: string
}

export type RuntimeMode = 'redis' | 'memory' | 'memory_fallback'

export type RuntimeSnapshot = {
  mode: RuntimeMode
  current_concurrency: number
  rpm: number
}

export type ChannelStatusEvent = {
  id: number
  channel_id: number
  scope: 'channel' | 'key'
  key_index?: number
  key_fingerprint?: string
  from_status: number
  to_status: number
  channel_status_before: number
  channel_status_after: number
  source: string
  reason_code: string
  reason_detail: string
  previous_reason_code?: string
  previous_reason_detail?: string
  actor_user_id?: number
  request_id?: string
  created_at: number
}

export type ChannelMetricPercentiles = {
  sample_count: number
  p50_ms: number | null
  p95_ms: number | null
  p99_ms: number | null
  overflow: boolean
}

export type ChannelMetricSummary = {
  attempt_count: number
  rpm: number
  success_count: number
  error_count: number
  retry_count: number
  availability_rate: number
  error_rate: number
  first_response_monitored: number
  first_response_timeout: number
  first_response_intercept_rate: number
  average_latency_ms: number
  average_tps: number | null
  throughput_sample_count: number
  ttft: ChannelMetricPercentiles
  upstream_429_count: number
  upstream_4xx_count: number
  upstream_5xx_count: number
  upstream_timeout_count: number
  canceled_count: number
  stream_error_count: number
  other_error_count: number
  capacity_rejected_count: number
  concurrency_rejected_count: number
  rpm_rejected_count: number
  disabled_skip_count: number
  cooldown_skip_count: number
  peak_concurrency: number
}

export type ChannelOverviewItem = {
  channel_id: number
  channel_name: string
  channel_type: number
  status: number
  metrics: ChannelMetricSummary
  latest_status_event?: ChannelStatusEvent
}

export type ChannelOverviewResult = {
  enabled: boolean
  start_ts: number
  end_ts: number
  minute_retention_days: number
  hour_retention_days: number
  items: ChannelOverviewItem[]
}

export type ChannelRuntimeItem = {
  channel_id: number
  channel_name: string
  channel_type: number
  status: number
  max_concurrency: number
  rpm_limit: number
  runtime: RuntimeSnapshot
}

export type ChannelRuntimeResult = {
  enabled: boolean
  as_of_ts: number
  items: ChannelRuntimeItem[]
}

export type ChannelMetricSeriesPoint = {
  ts: number
  bucket_seconds: number
  peak_concurrency: number
  metrics: ChannelMetricSummary
}

export type ChannelErrorBreakdownItem = {
  http_status: number
  error_code: string
  count: number
}

export type ChannelDetailResult = {
  enabled: boolean
  start_ts: number
  end_ts: number
  minute_retention_days: number
  hour_retention_days: number
  channel_id: number
  channel_name: string
  channel_type: number
  status: number
  summary: ChannelMetricSummary
  series: ChannelMetricSeriesPoint[]
  error_breakdown: ChannelErrorBreakdownItem[]
  status_events: ChannelStatusEvent[]
}

export type ChannelDimensionResult = {
  start_ts: number
  end_ts: number
  groups: string[]
  models: string[]
  endpoints: string[]
}

export type ChannelMonitoringResponse<T> = {
  success: boolean
  message?: string
  data: T
}

export type StatusTimelineSegment = {
  status: number
  startTs: number
  endTs: number
  widthPercent: number
}
