/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.

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

import { CHANNEL_FORM_DEFAULT_VALUES, channelFormSchema } from '../channel-form'
import {
  hasAdvancedSettingsErrors,
  isAdvancedSettingsField,
} from '../channel-form-errors'

const channelTestSettingFields = [
  'test_endpoint_type',
  'test_stream',
  'test_sample_tokens',
  'test_prepend_nonce',
  'test_disable_threshold_seconds',
] as const

describe('channel form advanced settings errors', () => {
  test('recognizes every channel test setting as advanced', () => {
    for (const field of channelTestSettingFields) {
      assert.equal(isAdvancedSettingsField(field), true)
      assert.equal(
        hasAdvancedSettingsErrors({ [field]: { type: 'manual' } }),
        true
      )
    }
  })

  test('recognizes schema failures for sample size and disable threshold', () => {
    const parsed = channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'channel',
      key: 'key',
      models: 'gpt-test',
      group: ['default'],
      test_sample_tokens: 8193,
      test_disable_threshold_seconds: -0.1,
    })

    assert.equal(parsed.success, false)
    if (parsed.success) return

    const errors = Object.fromEntries(
      parsed.error.issues.map((issue) => [
        String(issue.path[0]),
        { message: issue.message },
      ])
    )
    assert.equal(hasAdvancedSettingsErrors(errors), true)
    assert.ok(errors.test_sample_tokens)
    assert.ok(errors.test_disable_threshold_seconds)
  })
})
