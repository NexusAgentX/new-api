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
import { after, describe, test } from 'node:test'

import {
  convertDisplayAmountToUSD,
  convertUSDToDisplayAmount,
} from '@/lib/currency'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
  type CurrencyConfig,
} from '@/stores/system-config-store'

import { formatChannelFinanceMoney } from '../finance-format'

const originalCurrency = {
  ...useSystemConfigStore.getState().config.currency,
}

function setCurrency(overrides: Partial<CurrencyConfig>): void {
  useSystemConfigStore.setState((state) => ({
    config: {
      ...state.config,
      currency: {
        ...DEFAULT_CURRENCY_CONFIG,
        ...overrides,
      },
    },
  }))
}

after(() => {
  useSystemConfigStore.setState((state) => ({
    config: {
      ...state.config,
      currency: originalCurrency,
    },
  }))
})

describe('channel finance display currency', () => {
  test('converts display input values to and from stored USD', () => {
    const cases: Array<{
      currency: Partial<CurrencyConfig>
      usd: number
      display: number
    }> = [
      { currency: { quotaDisplayType: 'USD' }, usd: 2.5, display: 2.5 },
      {
        currency: { quotaDisplayType: 'CNY', usdExchangeRate: 7 },
        usd: 2.5,
        display: 17.5,
      },
      {
        currency: {
          quotaDisplayType: 'CUSTOM',
          customCurrencyExchangeRate: 0.9,
        },
        usd: 2.5,
        display: 2.25,
      },
      {
        currency: { quotaDisplayType: 'TOKENS', quotaPerUnit: 500_000 },
        usd: 2.5,
        display: 1_250_000,
      },
    ]

    for (const item of cases) {
      setCurrency(item.currency)
      assert.equal(convertUSDToDisplayAmount(item.usd), item.display)
      assert.equal(convertDisplayAmountToUSD(item.display), item.usd)
    }
  })

  test('formats finance values in token display mode instead of USD', () => {
    setCurrency({ quotaDisplayType: 'TOKENS', quotaPerUnit: 500_000 })

    assert.equal(formatChannelFinanceMoney(2), '1000000')
  })
})
