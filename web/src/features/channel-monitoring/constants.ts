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
export const CHANNEL_MONITORING_TIME_RANGES = [
  { value: 1, labelKey: 'Last hour' },
  { value: 6, labelKey: 'Last 6 hours' },
  { value: 24, labelKey: 'Last 24 hours' },
  { value: 168, labelKey: 'Last 7 days' },
  { value: 720, labelKey: 'Last 30 days' },
  { value: 2160, labelKey: 'Last 90 days' },
] as const

export const CHANNEL_STATUS_SOURCE_LABELS: Record<string, string> = {
  balance_check: 'Balance check',
  channel_test: 'Channel test',
  first_response_policy: 'First-response policy',
  legacy: 'Legacy status change',
  legacy_recovery: 'Legacy recovery',
  manual: 'Manual operation',
  manual_batch: 'Manual batch operation',
  manual_tag: 'Manual tag operation',
  passive_recovery_test: 'Passive recovery test',
  relay_error: 'Upstream relay',
  scheduled_channel_test: 'Scheduled channel test',
  tag_operation: 'Tag operation',
  task_account_pool: 'Task account pool',
}

export const CHANNEL_STATUS_REASON_LABELS: Record<string, string> = {
  all_accounts_unavailable: 'All accounts unavailable',
  all_keys_disabled: 'All keys disabled',
  channel_test_failed: 'Channel test failed',
  channel_test_succeeded: 'Channel test succeeded',
  insufficient_balance: 'Insufficient balance',
  first_response_timeout_rate: 'First-response timeout threshold',
  manual_disable: 'Manually disabled',
  manual_enable: 'Manually enabled',
  manual_tag_disable: 'Disabled by tag',
  manual_tag_enable: 'Enabled by tag',
  passive_recovery_succeeded: 'Passive recovery succeeded',
  recovery_succeeded: 'Recovery succeeded',
  recovery_test_succeeded: 'Recovery test succeeded',
  status_changed: 'Status changed',
  upstream_error: 'Upstream error',
}

export const STATUS_TIMELINE_COLORS: Record<number, string> = {
  0: 'bg-zinc-400',
  1: 'bg-emerald-500',
  2: 'bg-rose-500',
  3: 'bg-amber-500',
}
