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
import { after, test } from 'node:test'

import { Window } from 'happy-dom'

import type { ChannelFinanceChannel } from '../../../types'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { ChannelFinanceTable } = await import('../channel-finance-table')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'Channel Profitability': 'Channel Profitability',
        Channel: 'Channel',
        'Cost Mode': 'Cost Mode',
        Requests: 'Requests',
        Revenue: 'Revenue',
        'Variable Cost': 'Variable Cost',
        'Fixed Cost': 'Fixed Cost',
        'Total Cost': 'Total Cost',
        'Gross Profit': 'Gross Profit',
        'Gross Margin': 'Gross Margin',
        'Usage Cost Ratio': 'Usage Cost Ratio',
        'Channel test cost': 'Channel test cost',
        '{{count}} tests': '{{count}} tests',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

after(() => {
  domWindow.close()
})

test('shows channel-test requests and cost separately from customer values', async () => {
  const channel: ChannelFinanceChannel = {
    channel_id: 30,
    channel_name: 'xtoken-special',
    deleted: false,
    cost_mode: 'usage_ratio',
    cost_setting: 0.08,
    revenue_usd: 10,
    variable_cost_usd: 2,
    fixed_cost_usd: 0,
    channel_cost_usd: 2,
    infrastructure_cost_usd: 0,
    checkin_cost_usd: 0,
    cost_usd: 2,
    profit_usd: 8,
    margin: 0.8,
    request_count: 12,
    test_request_count: 2,
    test_cost_usd: 0.5,
    missing_revenue_count: 0,
    missing_cost_count: 0,
    missing_test_cost_count: 0,
    periods: [],
  }
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <ChannelFinanceTable channels={[channel]} />
      </I18nextProvider>
    )
  })

  assert.equal(container.textContent?.includes('12'), true)
  assert.equal(container.textContent?.includes('2 tests'), true)
  assert.equal(container.textContent?.includes('Channel test cost'), true)

  await act(async () => root.unmount())
  container.remove()
})
