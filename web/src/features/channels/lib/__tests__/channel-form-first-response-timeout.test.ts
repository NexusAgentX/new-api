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

function createChannel(setting: string): Channel {
  return {
    id: 1,
    type: 1,
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

describe('channel first-response timeout form', () => {
  test('restores and serializes a channel override', () => {
    const channel = createChannel('{"first_response_timeout_seconds":42}')
    const values = transformChannelToFormDefaults(channel)
    assert.equal(values.first_response_timeout_seconds, 42)

    const result = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'channel',
      key: 'key',
      models: 'gpt-test',
      group: ['default'],
      first_response_timeout_seconds: 42,
    })
    assert.equal(
      JSON.parse(String(result.channel.setting)).first_response_timeout_seconds,
      42
    )
  })

  test('omits a cleared override from the setting JSON', () => {
    const result = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'channel',
      key: 'key',
      models: 'gpt-test',
      group: ['default'],
      first_response_timeout_seconds: undefined,
    })
    assert.equal(
      Object.hasOwn(
        JSON.parse(String(result.channel.setting)),
        'first_response_timeout_seconds'
      ),
      false
    )
  })
})
