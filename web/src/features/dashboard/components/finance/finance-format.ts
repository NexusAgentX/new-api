/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your option)
any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useCallback } from 'react'

import {
  formatCurrencyFromUSD,
  formatCurrencyFromUSDWithConfig,
} from '@/lib/currency'
import {
  useSystemConfigStore,
  type CurrencyConfig,
} from '@/stores/system-config-store'

const MONEY_FORMAT_OPTIONS = {
  digitsLarge: 2,
  digitsSmall: 6,
  abbreviate: false,
} as const

export function formatChannelFinanceMoney(
  value: number,
  currency?: CurrencyConfig
): string {
  if (currency) {
    return formatCurrencyFromUSDWithConfig(
      value,
      currency,
      MONEY_FORMAT_OPTIONS
    )
  }
  return formatCurrencyFromUSD(value, MONEY_FORMAT_OPTIONS)
}

export function useChannelFinanceMoneyFormatter(): (value: number) => string {
  const currency = useSystemConfigStore((state) => state.config.currency)
  return useCallback(
    (value: number) => formatChannelFinanceMoney(value, currency),
    [currency]
  )
}

export function formatChannelFinanceMargin(
  value: number,
  revenue: number
): string {
  if (revenue === 0 || !Number.isFinite(value)) return '-'
  return Intl.NumberFormat(undefined, {
    style: 'percent',
    maximumFractionDigits: 2,
  }).format(value)
}
