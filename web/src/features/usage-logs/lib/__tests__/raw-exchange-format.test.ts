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

/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { TFunction } from 'i18next'

import type { LogOtherData } from '../../types'
import { renderAuditContent } from '../format'
import {
  formatRawExchangeBytes,
  rawExchangeCaptureModeLabel,
  rawExchangeOutcomeLabel,
} from '../raw-exchange-format'

const translate = ((key: string) => key) as TFunction

describe('raw exchange audit formatting', () => {
  const auditTranslate = (key: string, params: Record<string, unknown> = {}) =>
    key.replaceAll(/{{\s*([^}\s]+)\s*}}/g, (_, name: string) =>
      String(params[name])
    )

  test('renders signed cleanup audit counts including zero', () => {
    const other: LogOtherData = {
      op: {
        action: 'raw_exchange.cleanup_preview',
        params: { count: 0 },
      },
    }
    assert.equal(
      renderAuditContent(other, auditTranslate),
      'Previewed cleanup for 0 raw exchange archives'
    )
  })

  test('renders the terminal cleanup task result', () => {
    const other: LogOtherData = {
      op: {
        action: 'raw_exchange.cleanup_complete',
        params: {
          task_id: 'cleanup-task',
          status: 'succeeded',
          deleted: 2,
          failed: 0,
        },
      },
    }
    assert.equal(
      renderAuditContent(other, auditTranslate),
      'Completed raw exchange cleanup task cleanup-task with status succeeded: 2 deleted, 0 failed'
    )
  })
})

describe('raw exchange summary formatting', () => {
  test('formats logical and stored byte counts without losing unit boundaries', () => {
    assert.equal(formatRawExchangeBytes(0), '0 B')
    assert.equal(formatRawExchangeBytes(1023), '1023 B')
    assert.equal(formatRawExchangeBytes(1024), '1.00 KB')
    assert.equal(formatRawExchangeBytes(1536), '1.50 KB')
    assert.equal(formatRawExchangeBytes(Number.NaN), '0 B')
  })

  test('maps capture modes and outcomes to stable user-facing labels', () => {
    assert.equal(rawExchangeCaptureModeLabel('off', translate), 'Off')
    assert.equal(
      rawExchangeCaptureModeLabel('non_success', translate),
      'Non-success'
    )
    assert.equal(rawExchangeCaptureModeLabel('all', translate), 'All requests')
    assert.equal(rawExchangeOutcomeLabel('success', translate), 'Success')
    assert.equal(
      rawExchangeOutcomeLabel('non_success', translate),
      'Non-success'
    )
  })
})
