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
import { api } from '@/lib/api'

import type {
  ChannelDetailResult,
  ChannelDimensionResult,
  ChannelMonitoringQueryParams,
  ChannelMonitoringResponse,
  ChannelOverviewResult,
  ChannelRuntimeResult,
} from './types'

export async function getChannelMetricsOverview(
  params: ChannelMonitoringQueryParams
): Promise<ChannelMonitoringResponse<ChannelOverviewResult>> {
  const response = await api.get<
    ChannelMonitoringResponse<ChannelOverviewResult>
  >('/api/channel/metrics/overview', { params })
  return response.data
}

export async function getChannelMetricDimensions(
  params: Pick<ChannelMonitoringQueryParams, 'hours' | 'channel_id'>
): Promise<ChannelMonitoringResponse<ChannelDimensionResult>> {
  const response = await api.get<
    ChannelMonitoringResponse<ChannelDimensionResult>
  >('/api/channel/metrics/dimensions', { params })
  return response.data
}

export async function getChannelMetricsRuntime(): Promise<
  ChannelMonitoringResponse<ChannelRuntimeResult>
> {
  const response = await api.get<
    ChannelMonitoringResponse<ChannelRuntimeResult>
  >('/api/channel/metrics/runtime')
  return response.data
}

export async function getChannelMetricsDetail(
  channelId: number,
  params: ChannelMonitoringQueryParams
): Promise<ChannelMonitoringResponse<ChannelDetailResult>> {
  const response = await api.get<
    ChannelMonitoringResponse<ChannelDetailResult>
  >(`/api/channel/metrics/${channelId}`, { params })
  return response.data
}
