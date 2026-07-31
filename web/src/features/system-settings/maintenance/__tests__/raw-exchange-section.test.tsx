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
import assert from 'node:assert/strict'
import { after, beforeEach, describe, test } from 'node:test'

import { AxiosHeaders, type AxiosAdapter, type AxiosRequestConfig } from 'axios'
import { Window } from 'happy-dom'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'FocusEvent',
  'KeyboardEvent',
  'MouseEvent',
  'PointerEvent',
  'MutationObserver',
  'ResizeObserver',
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
const { api } = await import('@/lib/api')
const { RawExchangeSection } = await import('../raw-exchange-section')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const originalAdapter = api.defaults.adapter
let requestHandler: (config: AxiosRequestConfig) => Promise<unknown> | unknown

const testAdapter: AxiosAdapter = async (config) => ({
  data: await requestHandler(config),
  status: 200,
  statusText: 'OK',
  headers: new AxiosHeaders(),
  config,
})

function statsResponse() {
  return {
    success: true,
    message: '',
    data: {
      stats: {
        total_count: 0,
        request_bytes: 0,
        response_bytes: 0,
        request_stored_bytes: 0,
        response_stored_bytes: 0,
        used_bytes: 0,
        oldest_created_at: 0,
        newest_created_at: 0,
        statuses: [],
        top_users: [],
        top_tokens: [],
      },
      capture_enabled: true,
      storage_ready: true,
      storage_backend: 's3',
      retention_days: 7,
      capacity_bytes: 1024,
      max_request_bytes: 256,
      max_response_bytes: 256,
      max_exchange_bytes: 512,
      latest_gc: null,
    },
  }
}

function cleanupTask(status: 'running' | 'succeeded') {
  return {
    id: 1,
    task_id: 'raw-cleanup-task',
    type: 'raw_exchange_cleanup',
    status,
    payload: { filter: {}, batch_size: 100 },
    state:
      status === 'running'
        ? { total: 2, processed: 1, progress: 50, remaining: 1 }
        : { total: 2, processed: 2, progress: 100, remaining: 0 },
    result:
      status === 'succeeded'
        ? {
            matched: 2,
            processed: 2,
            deleted: 2,
            already_deleted: 0,
            skipped: 0,
            failed: 0,
            request_bytes_freed: 40,
            response_bytes_freed: 20,
            total_bytes_freed: 60,
          }
        : undefined,
    created_at: 1,
    updated_at: 2,
  }
}

type RenderedSection = {
  container: HTMLDivElement
  root: ReturnType<typeof createRoot>
}

async function flushEffects() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
  })
}

async function renderSection(flush = true): Promise<RenderedSection> {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <RawExchangeSection />
      </I18nextProvider>
    )
  })
  if (flush) await flushEffects()
  return { container, root }
}

async function unmountSection(rendered: RenderedSection) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

function buttonByText(text: string): HTMLButtonElement {
  const button = [
    ...document.querySelectorAll<HTMLButtonElement>('button'),
  ].find((candidate) => candidate.textContent?.trim().endsWith(text))
  assert.ok(button, `expected button ${text}`)
  return button
}

async function click(element: Element) {
  await act(async () => {
    element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await Promise.resolve()
  })
}

async function fill(input: HTMLInputElement, value: string) {
  await act(async () => {
    const setter = Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      'value'
    )?.set
    setter?.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
    await Promise.resolve()
  })
}

describe('raw exchange maintenance section', () => {
  beforeEach(() => {
    document.body.replaceChildren()
    api.defaults.adapter = testAdapter
  })

  after(() => {
    api.defaults.adapter = originalAdapter
    domWindow.close()
  })

  test('shows loading and then stable empty usage states', async () => {
    let resolveStats: ((value: unknown) => void) | undefined
    const pendingStats = new Promise((resolve) => {
      resolveStats = resolve
    })
    requestHandler = (config) => {
      if (config.url === '/api/raw-exchanges/admin/stats') return pendingStats
      if (config.url === '/api/system-task/list') {
        return { success: true, message: '', data: [] }
      }
      throw new Error(`unexpected request ${config.url}`)
    }

    const rendered = await renderSection(false)
    assert.equal(buttonByText('Refresh').disabled, true)
    resolveStats?.(statsResponse())
    await flushEffects()

    assert.equal(buttonByText('Refresh').disabled, false)
    assert.match(document.body.textContent ?? '', /No usage data yet\./)
    assert.match(document.body.textContent ?? '', /0 B/)
    await unmountSection(rendered)
  })

  test('previews an all-archive cleanup and requires the exact confirmation', async () => {
    requestHandler = (config) => {
      if (config.url === '/api/raw-exchanges/admin/stats') {
        return statsResponse()
      }
      if (config.url === '/api/system-task/list') {
        return { success: true, message: '', data: [] }
      }
      if (config.url === '/api/raw-exchanges/admin/cleanup-preview') {
        return {
          success: true,
          message: '',
          data: {
            preview: {
              count: 2,
              request_stored_bytes: 40,
              response_stored_bytes: 20,
              total_stored_bytes: 60,
            },
            preview_token: 'signed-preview',
            expires_at: 100,
          },
        }
      }
      throw new Error(`unexpected request ${config.url}`)
    }
    const rendered = await renderSection()

    await click(buttonByText('Clear date for all archives'))
    await click(buttonByText('Preview cleanup'))
    await flushEffects()
    assert.match(document.body.textContent ?? '', /2 archives/)
    await click(buttonByText('Start cleanup'))

    const confirmation = document.querySelector<HTMLInputElement>(
      'input[placeholder="DELETE ALL RAW EXCHANGES"]'
    )
    assert.ok(confirmation)
    const confirmButton = buttonByText('Confirm cleanup')
    assert.equal(confirmButton.disabled, true)
    await fill(confirmation, 'DELETE ALL')
    assert.equal(confirmButton.disabled, true)
    await fill(confirmation, 'DELETE ALL RAW EXCHANGES')
    assert.equal(confirmButton.disabled, false)

    await unmountSection(rendered)
  })

  test('discovers an active task and polls it through its terminal result', async () => {
    let taskReads = 0
    requestHandler = (config) => {
      if (config.url === '/api/raw-exchanges/admin/stats') {
        return statsResponse()
      }
      if (config.url === '/api/system-task/list') {
        return { success: true, message: '', data: [cleanupTask('running')] }
      }
      if (config.url === '/api/system-task/raw-cleanup-task') {
        taskReads += 1
        return {
          success: true,
          message: '',
          data: cleanupTask(taskReads > 1 ? 'succeeded' : 'running'),
        }
      }
      throw new Error(`unexpected request ${config.url}`)
    }
    const intervalCallbacks: Array<() => void> = []
    const originalSetInterval = window.setInterval
    const originalClearInterval = window.clearInterval
    window.setInterval = ((callback: TimerHandler) => {
      intervalCallbacks.push(callback as () => void)
      return 1
    }) as typeof window.setInterval
    window.clearInterval = (() => undefined) as typeof window.clearInterval

    const rendered = await renderSection()
    assert.match(document.body.textContent ?? '', /Running/)
    assert.equal(intervalCallbacks.length, 1)
    await act(async () => {
      intervalCallbacks[0]?.()
      await Promise.resolve()
      await Promise.resolve()
    })
    await flushEffects()

    assert.match(document.body.textContent ?? '', /Success/)
    assert.match(document.body.textContent ?? '', /Deleted: 2/)
    assert.match(document.body.textContent ?? '', /Total: 60 B/)

    window.setInterval = originalSetInterval
    window.clearInterval = originalClearInterval
    await unmountSection(rendered)
  })

  test('recovers from a failed preview without retaining stale result state', async () => {
    requestHandler = (config) => {
      if (config.url === '/api/raw-exchanges/admin/stats') {
        return statsResponse()
      }
      if (config.url === '/api/system-task/list') {
        return { success: true, message: '', data: [] }
      }
      if (config.url === '/api/raw-exchanges/admin/cleanup-preview') {
        return { success: false, message: 'preview failed' }
      }
      throw new Error(`unexpected request ${config.url}`)
    }
    const rendered = await renderSection()

    await click(buttonByText('Preview cleanup'))
    await flushEffects()

    assert.doesNotMatch(document.body.textContent ?? '', /Cleanup preview/)
    assert.equal(buttonByText('Preview cleanup').disabled, false)
    await unmountSection(rendered)
  })
})
