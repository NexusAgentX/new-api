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

import type { Channel } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../channel-form'

function createChannel(setting: string, type = 1): Channel {
  return {
    id: 1,
    type,
    key: '',
    status: 1,
    name: 'channel',
    created_time: 1,
    test_time: 0,
    response_time: 0,
    base_url: null,
    other: '',
    balance: 0,
    balance_updated_time: 0,
    models: 'gpt-test',
    group: 'default',
    used_quota: 0,
    model_mapping: null,
    status_code_mapping: null,
    auto_ban: 1,
    other_info: '',
    priority: 0,
    weight: 0,
    openai_organization: null,
    test_model: null,
    tag: null,
    setting,
    param_override: null,
    header_override: null,
    remark: '',
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    cost_mode: 'none',
    fixed_daily_cost_usd: 0,
    usage_cost_ratio: 0,
    settings: '{}',
    max_input_tokens: 0,
  }
}

function createPayloadSettings(
  overrides: Partial<typeof CHANNEL_FORM_DEFAULT_VALUES> = {}
): Record<string, unknown> {
  const payload = transformFormDataToCreatePayload({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'channel',
    key: 'key',
    models: 'gpt-test',
    group: ['default'],
    ...overrides,
  })
  return JSON.parse(String(payload.channel.setting)) as Record<string, unknown>
}

describe('channel Responses compatibility form', () => {
  test('defaults compatibility on and summary-only reasoning off for legacy channels', () => {
    assert.equal(CHANNEL_FORM_DEFAULT_VALUES.responses_compatibility_fix, true)
    assert.equal(
      CHANNEL_FORM_DEFAULT_VALUES.allow_reasoning_without_encrypted_content,
      false
    )

    const values = transformChannelToFormDefaults(createChannel('{}'))
    assert.equal(values.responses_compatibility_fix, true)
    assert.equal(values.allow_reasoning_without_encrypted_content, false)

    const settings = createPayloadSettings()
    assert.equal(settings.responses_compatibility_fix, true)
    assert.equal(settings.allow_reasoning_without_encrypted_content, false)
  })

  test('round-trips explicit main and dependent policy values', () => {
    const values = transformChannelToFormDefaults(
      createChannel(
        '{"responses_compatibility_fix":false,"allow_reasoning_without_encrypted_content":true}'
      )
    )
    assert.equal(values.responses_compatibility_fix, false)
    assert.equal(values.allow_reasoning_without_encrypted_content, true)

    const settings = createPayloadSettings({
      responses_compatibility_fix: false,
      allow_reasoning_without_encrypted_content: true,
    })
    assert.equal(settings.responses_compatibility_fix, false)
    assert.equal(settings.allow_reasoning_without_encrypted_content, true)
  })

  test('omits Responses compatibility settings for non-OpenAI channel types', () => {
    const settings = createPayloadSettings({
      type: 14,
      responses_compatibility_fix: false,
      allow_reasoning_without_encrypted_content: true,
    })
    assert.equal(Object.hasOwn(settings, 'responses_compatibility_fix'), false)
    assert.equal(
      Object.hasOwn(settings, 'allow_reasoning_without_encrypted_content'),
      false
    )
  })
})
