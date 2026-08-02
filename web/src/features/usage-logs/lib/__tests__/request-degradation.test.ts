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

import type { LogOtherData } from '../../types'
import { getRequestDegradation, getResponsesCompatibility } from '../format'

describe('Responses compatibility log metadata', () => {
  test('accepts aggregate compatibility counts without sensitive fields', () => {
    const compatibility = getResponsesCompatibility({
      responses_compatibility: {
        dropped_non_replayable_reasoning_items: 4,
        normalized_request_item_ids: 2,
        normalized_response_item_ids: 3,
      },
    })

    assert.deepEqual(compatibility, {
      dropped_non_replayable_reasoning_items: 4,
      normalized_request_item_ids: 2,
      normalized_response_item_ids: 3,
    })
  })

  test('maps legacy reasoning degradation logs to the compatibility view', () => {
    assert.deepEqual(
      getResponsesCompatibility({
        request_degradation: {
          applied: true,
          reason: 'non_replayable_reasoning',
          dropped_reasoning_items: 4,
        },
      }),
      {
        dropped_non_replayable_reasoning_items: 4,
        normalized_request_item_ids: 0,
        normalized_response_item_ids: 0,
      }
    )
  })

  test('accepts the stable non-replayable reasoning contract', () => {
    const degradation = getRequestDegradation({
      request_degradation: {
        applied: true,
        reason: 'non_replayable_reasoning',
        dropped_reasoning_items: 4,
      },
    })

    assert.deepEqual(degradation, {
      applied: true,
      reason: 'non_replayable_reasoning',
      dropped_reasoning_items: 4,
    })
  })

  test('rejects incomplete, unapplied, and unknown degradation metadata', () => {
    const invalidCases: Array<LogOtherData | null> = [
      null,
      {},
      {
        request_degradation: {
          applied: false,
          reason: 'non_replayable_reasoning',
          dropped_reasoning_items: 1,
        },
      },
      {
        request_degradation: {
          applied: true,
          reason: 'non_replayable_reasoning',
          dropped_reasoning_items: 0,
        },
      },
      {
        request_degradation: {
          applied: true,
          reason: 'unknown_reason',
          dropped_reasoning_items: 1,
        },
      },
    ]

    for (const other of invalidCases) {
      assert.equal(getRequestDegradation(other), null)
    }
  })
})
