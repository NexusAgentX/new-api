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

import { Window } from 'happy-dom'
import type React from 'react'

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

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        Degraded: 'Degraded',
        'Removed non-replayable reasoning items: {{count}}':
          'Removed non-replayable reasoning items: {{count}}',
      },
    },
  },
})

const { RequestDegradationBadge } = await import('../request-degradation-badge')
const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

type RenderedBadge = {
  container: HTMLDivElement
  root: ReturnType<typeof createRoot>
}

async function renderBadge(
  props: React.ComponentProps<typeof RequestDegradationBadge>
): Promise<RenderedBadge> {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <RequestDegradationBadge {...props} />
      </I18nextProvider>
    )
  })

  return { container, root }
}

async function unmountBadge(rendered: RenderedBadge) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

describe('request degradation badge', () => {
  after(() => {
    domWindow.close()
  })

  test('shows a warning with the dropped reasoning count', async () => {
    const rendered = await renderBadge({
      other: {
        request_degradation: {
          applied: true,
          reason: 'non_replayable_reasoning',
          dropped_reasoning_items: 4,
        },
      },
    })

    const badge = rendered.container.querySelector(
      '[data-request-degradation-badge="true"]'
    )
    assert.ok(badge)
    assert.equal(rendered.container.textContent?.includes('Degraded'), true)
    assert.equal(
      badge.getAttribute('aria-label'),
      'Degraded: Removed non-replayable reasoning items: 4'
    )
    assert.equal(badge.getAttribute('tabindex'), '0')

    await unmountBadge(rendered)
  })

  test('renders nothing when no degradation was applied', async () => {
    const rendered = await renderBadge({ other: {} })

    assert.equal(rendered.container.textContent, '')
    assert.equal(
      rendered.container.querySelector(
        '[data-request-degradation-badge="true"]'
      ),
      null
    )

    await unmountBadge(rendered)
  })
})
