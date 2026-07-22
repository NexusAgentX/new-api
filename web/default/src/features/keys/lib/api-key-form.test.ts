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

import type { ApiKey } from '../types'
import {
  getApiKeyFormDefaultValues,
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
} from './api-key-form'

function createApiKey(
  requestCustomization: string,
  autoGroupPolicy = ''
): ApiKey {
  return {
    id: 1,
    name: 'policy-key',
    key: 'masked',
    status: 1,
    remain_quota: 0,
    used_quota: 0,
    unlimited_quota: true,
    expired_time: -1,
    created_time: 1,
    accessed_time: 1,
    group: 'auto',
    cross_group_retry: true,
    model_limits_enabled: false,
    model_limits: '',
    auto_group_policy: autoGroupPolicy,
    request_customization: requestCustomization,
    allow_ips: '',
  }
}

describe('API key auto-group policy form', () => {
  test('serializes model mapping and auto-group policy separately', () => {
    const values = getApiKeyFormDefaultValues(true)
    values.model_mapping = '{"client-model":"gateway-model"}'
    values.auto_group_policy = {
      enabled: true,
      default_rule: {
        mode: 'denylist',
        groups: ['premium'],
        max_ratio: 1.5,
      },
      model_rules: [
        {
          id: 'rule-1',
          model: 'gateway-model',
          mode: 'allowlist',
          groups: ['preferred'],
        },
      ],
    }

    const payload = transformFormDataToPayload(values)
    assert.deepEqual(JSON.parse(payload.request_customization), {
      version: 1,
      model_mapping: { 'client-model': 'gateway-model' },
    })
    assert.deepEqual(JSON.parse(payload.auto_group_policy), {
      default_rule: {
        mode: 'denylist',
        groups: ['premium'],
        max_ratio: 1.5,
      },
      model_rules: {
        'gateway-model': {
          mode: 'allowlist',
          groups: ['preferred'],
        },
      },
    })
  })

  test('restores a persisted policy for editing', () => {
    const apiKey = createApiKey(
      '',
      JSON.stringify({
        default_rule: { min_ratio: 0, max_ratio: 1.5 },
        model_rules: {
          'gpt-4o': { mode: 'denylist', groups: ['premium'] },
        },
      })
    )

    const values = transformApiKeyToFormDefaults(apiKey)
    assert.equal(values.auto_group_policy.enabled, true)
    assert.equal(values.auto_group_policy.default_rule.min_ratio, 0)
    assert.equal(values.auto_group_policy.default_rule.max_ratio, 1.5)
    assert.deepEqual(values.auto_group_policy.model_rules, [
      {
        id: 'gpt-4o',
        model: 'gpt-4o',
        mode: 'denylist',
        groups: ['premium'],
        min_ratio: undefined,
        max_ratio: undefined,
      },
    ])
  })

  test('omits an empty policy', () => {
    const values = getApiKeyFormDefaultValues(true)
    values.auto_group_policy.enabled = true

    const payload = transformFormDataToPayload(values)
    assert.equal(payload.auto_group_policy, '')
    assert.equal(payload.request_customization, '')
  })
})
