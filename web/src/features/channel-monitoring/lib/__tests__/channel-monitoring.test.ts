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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { ChannelStatusEvent } from '../../types'
import {
  buildChannelMonitoringQuery,
  buildStatusTimelineSegments,
  formatChannelEventRelativeTime,
  statusReasonLabelKey,
} from '../channel-monitoring'

function event(
  id: number,
  createdAt: number,
  before: number,
  after: number
): ChannelStatusEvent {
  return {
    id,
    channel_id: 7,
    scope: 'channel',
    from_status: before,
    to_status: after,
    channel_status_before: before,
    channel_status_after: after,
    source: 'manual',
    reason_code: 'status_changed',
    reason_detail: '',
    created_at: createdAt,
  }
}

describe('channel monitoring query and timeline', () => {
  test('omits empty filters while preserving the selected channel', () => {
    assert.deepEqual(
      buildChannelMonitoringQuery({
        hours: 24,
        channel: 9,
        group: '',
        model: 'gpt-test',
      }),
      {
        hours: 24,
        channel_id: 9,
        model: 'gpt-test',
      }
    )
  })

  test('builds status segments in chronological order and merges adjacent states', () => {
    const segments = buildStatusTimelineSegments(
      [event(3, 180, 3, 1), event(1, 120, 1, 3), event(2, 150, 3, 3)],
      100,
      200,
      1
    )

    assert.deepEqual(
      segments.map((segment) => ({
        status: segment.status,
        startTs: segment.startTs,
        endTs: segment.endTs,
        widthPercent: segment.widthPercent,
      })),
      [
        { status: 1, startTs: 100, endTs: 120, widthPercent: 20 },
        { status: 3, startTs: 120, endTs: 180, widthPercent: 60 },
        { status: 1, startTs: 180, endTs: 200, widthPercent: 20 },
      ]
    )
  })

  test('normalizes internal Chinese language codes before relative time formatting', () => {
    assert.doesNotThrow(() =>
      formatChannelEventRelativeTime(1_700_000_000, 'zhCN')
    )
  })

  test('keeps a stable translated category for provider HTTP reason codes', () => {
    assert.equal(
      statusReasonLabelKey('upstream_http_429'),
      'Upstream HTTP error'
    )
  })
})
