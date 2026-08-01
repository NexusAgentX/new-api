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
import { toIntlLocale } from '@/i18n/languages'
import { formatTimestampRelative } from '@/lib/format'

import {
  CHANNEL_STATUS_REASON_LABELS,
  CHANNEL_STATUS_SOURCE_LABELS,
} from '../constants'
import type {
  ChannelMonitoringQueryParams,
  ChannelMonitoringSearch,
  ChannelStatusEvent,
  StatusTimelineSegment,
} from '../types'

export function buildChannelMonitoringQuery(
  search: ChannelMonitoringSearch,
  channelId?: number
): ChannelMonitoringQueryParams {
  const params: ChannelMonitoringQueryParams = { hours: search.hours }
  const selectedChannel = channelId ?? search.channel
  if (selectedChannel) params.channel_id = selectedChannel
  if (search.group) params.group = search.group
  if (search.model) params.model = search.model
  if (search.endpoint) params.endpoint = search.endpoint
  return params
}

export function buildStatusTimelineSegments(
  events: ChannelStatusEvent[],
  startTs: number,
  endTs: number,
  currentStatus: number
): StatusTimelineSegment[] {
  if (endTs <= startTs) {
    return [
      {
        status: currentStatus,
        startTs,
        endTs,
        widthPercent: 100,
      },
    ]
  }
  const orderedEvents = events
    .filter((event) => event.created_at >= startTs && event.created_at <= endTs)
    .sort(
      (left, right) => left.created_at - right.created_at || left.id - right.id
    )
  let status = orderedEvents[0]?.channel_status_before ?? currentStatus
  let cursor = startTs
  const rawSegments: Array<Omit<StatusTimelineSegment, 'widthPercent'>> = []

  for (const event of orderedEvents) {
    const eventTs = Math.min(endTs, Math.max(startTs, event.created_at))
    if (eventTs > cursor) {
      rawSegments.push({ status, startTs: cursor, endTs: eventTs })
    }
    status = event.channel_status_after
    cursor = Math.max(cursor, eventTs)
  }
  if (cursor < endTs || rawSegments.length === 0) {
    rawSegments.push({ status, startTs: cursor, endTs })
  }

  const merged: Array<Omit<StatusTimelineSegment, 'widthPercent'>> = []
  for (const segment of rawSegments) {
    const previous = merged.at(-1)
    if (previous && previous.status === segment.status) {
      previous.endTs = segment.endTs
    } else {
      merged.push({ ...segment })
    }
  }
  const duration = endTs - startTs
  return merged.map((segment) => ({
    ...segment,
    widthPercent: ((segment.endTs - segment.startTs) / duration) * 100,
  }))
}

export function formatChannelEventRelativeTime(
  timestamp: number,
  language: string
): string {
  return formatTimestampRelative(timestamp, 'seconds', toIntlLocale(language))
}

export function statusSourceLabelKey(source: string): string {
  return CHANNEL_STATUS_SOURCE_LABELS[source] ?? 'Other status source'
}

export function statusReasonLabelKey(reasonCode: string): string {
  if (reasonCode.startsWith('upstream_http_')) return 'Upstream HTTP error'
  return CHANNEL_STATUS_REASON_LABELS[reasonCode] ?? 'Other status reason'
}
