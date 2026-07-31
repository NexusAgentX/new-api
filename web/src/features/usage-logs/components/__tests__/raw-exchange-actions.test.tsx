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
import { after, beforeEach, describe, test } from 'node:test'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { Table } from '@tanstack/react-table'
import { AxiosHeaders, type AxiosAdapter, type AxiosRequestConfig } from 'axios'
import { Window } from 'happy-dom'

import type { RawExchangeSummary } from '../../data/schema'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLAnchorElement',
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
const { RawExchangeActions } = await import('../raw-exchange-actions')
const { RawExchangeBulkActions } = await import('../raw-exchange-bulk-actions')

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

function archiveFixture(
  overrides: Partial<RawExchangeSummary> = {}
): RawExchangeSummary {
  return {
    request_id: 'raw-action-request',
    capture_mode: 'all',
    outcome: 'non_success',
    status: 'stored',
    http_status: 502,
    request_available: true,
    response_available: true,
    response_complete: true,
    request_bytes: 120,
    response_bytes: 80,
    request_stored_bytes: 120,
    response_stored_bytes: 80,
    created_at: 1,
    expires_at: 2,
    ...overrides,
  }
}

type RenderedActions = {
  container: HTMLDivElement
  root: ReturnType<typeof createRoot>
}

async function renderActions(
  archive: RawExchangeSummary,
  isAdmin = false
): Promise<RenderedActions> {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={queryClient}>
          <RawExchangeActions archive={archive} isAdmin={isAdmin} />
        </QueryClientProvider>
      </I18nextProvider>
    )
  })
  return { container, root }
}

async function renderBulkActions(
  table: Table<unknown>
): Promise<RenderedActions> {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={queryClient}>
          <RawExchangeBulkActions table={table} />
        </QueryClientProvider>
      </I18nextProvider>
    )
  })
  return { container, root }
}

function actionButton(label: string): HTMLButtonElement {
  const button = document.querySelector<HTMLButtonElement>(
    `button[aria-label="${label}"]`
  )
  assert.ok(button, `expected action button ${label}`)
  return button
}

async function flushEffects() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
  })
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

function buttonByText(text: string): HTMLButtonElement {
  const button = [
    ...document.querySelectorAll<HTMLButtonElement>('button'),
  ].find((candidate) => candidate.textContent?.trim().endsWith(text))
  assert.ok(button, `expected button ${text}`)
  return button
}

async function unmountActions(rendered: RenderedActions) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

describe('raw exchange actions', () => {
  beforeEach(() => {
    document.body.replaceChildren()
    api.defaults.adapter = testAdapter
  })

  after(() => {
    api.defaults.adapter = originalAdapter
    domWindow.close()
  })

  test('enables request and bundle while explaining an unavailable response', async () => {
    const rendered = await renderActions(
      archiveFixture({ response_available: false, response_bytes: 0 })
    )

    assert.equal(actionButton('Download request').disabled, false)
    assert.equal(actionButton('Download bundle').disabled, false)
    const response = actionButton('Download response')
    assert.equal(response.disabled, true)
    assert.equal(
      response.getAttribute('aria-description'),
      'No response was archived.'
    )
    assert.equal(actionButton('Delete raw exchange').disabled, false)

    await unmountActions(rendered)
  })

  test('keeps deleted archive actions visible but disabled with a reason', async () => {
    const rendered = await renderActions(
      archiveFixture({
        status: 'deleted',
        request_available: false,
        response_available: false,
        request_stored_bytes: 0,
        response_stored_bytes: 0,
      })
    )

    for (const label of [
      'Download request',
      'Download response',
      'Download bundle',
      'Delete raw exchange',
    ]) {
      const button = actionButton(label)
      assert.equal(button.disabled, true)
      assert.equal(
        button.getAttribute('aria-description'),
        'Archive has been deleted.'
      )
    }

    await unmountActions(rendered)
  })

  test('does not expose body or delete actions in the administrator view', async () => {
    const rendered = await renderActions(archiveFixture(), true)

    assert.equal(
      document.querySelector('button[aria-label="Download request"]'),
      null
    )
    assert.equal(
      document.querySelector('button[aria-label="Download response"]'),
      null
    )
    assert.equal(
      document.querySelector('button[aria-label="Download bundle"]'),
      null
    )
    assert.equal(
      document.querySelector('button[aria-label="Delete raw exchange"]'),
      null
    )

    await unmountActions(rendered)
  })

  test('downloads through 2FA and disables actions while the body is loading', async () => {
    let resolveDownload: ((value: unknown) => void) | undefined
    const pendingDownload = new Promise((resolve) => {
      resolveDownload = resolve
    })
    let proofHeader = ''
    requestHandler = (config) => {
      if (config.url === '/api/user/2fa/status') {
        return { success: true, data: { enabled: true } }
      }
      if (config.url === '/api/user/passkey') {
        return { success: true, data: { enabled: false } }
      }
      if (config.url === '/api/verify') {
        return { success: true, data: { proof_token: 'proof-token' } }
      }
      if (config.url?.endsWith('/request')) {
        proofHeader = String(
          (config.headers as AxiosHeaders).get('X-Security-Proof') ?? ''
        )
        return pendingDownload
      }
      throw new Error(`unexpected request ${config.url}`)
    }
    const downloaded: string[] = []
    const originalClick = HTMLAnchorElement.prototype.click
    HTMLAnchorElement.prototype.click = function () {
      downloaded.push(this.download)
    }
    const rendered = await renderActions(archiveFixture())

    await click(actionButton('Download request'))
    await flushEffects()
    const code = document.querySelector<HTMLInputElement>(
      'input[placeholder="Enter verification code"]'
    )
    assert.ok(code)
    await fill(code, '123456')
    await click(buttonByText('Verify'))
    await flushEffects()
    assert.equal(actionButton('Download request').disabled, true)
    assert.equal(proofHeader, 'proof-token')

    resolveDownload?.(new Blob(['request body']))
    await flushEffects()
    assert.equal(actionButton('Download request').disabled, false)
    assert.deepEqual(downloaded, ['raw-action-request-request.body'])

    HTMLAnchorElement.prototype.click = originalClick
    await unmountActions(rendered)
  })

  test('deletes through 2FA and leaves every archive action disabled', async () => {
    requestHandler = (config) => {
      if (config.url === '/api/user/2fa/status') {
        return { success: true, data: { enabled: true } }
      }
      if (config.url === '/api/user/passkey') {
        return { success: true, data: { enabled: false } }
      }
      if (config.url === '/api/verify') {
        return { success: true, data: { proof_token: 'delete-proof' } }
      }
      if (
        config.url === '/api/raw-exchanges/self/raw-action-request' &&
        config.method === 'delete'
      ) {
        assert.equal(
          (config.headers as AxiosHeaders).get('X-Security-Proof'),
          'delete-proof'
        )
        return { success: true, data: { deleted: true } }
      }
      throw new Error(`unexpected request ${config.method} ${config.url}`)
    }
    const rendered = await renderActions(archiveFixture())

    await click(actionButton('Delete raw exchange'))
    await click(buttonByText('Delete'))
    await flushEffects()
    const code = document.querySelector<HTMLInputElement>(
      'input[placeholder="Enter verification code"]'
    )
    assert.ok(code)
    await fill(code, '123456')
    await click(buttonByText('Verify'))
    await flushEffects()

    for (const label of [
      'Download request',
      'Download response',
      'Download bundle',
      'Delete raw exchange',
    ]) {
      const button = actionButton(label)
      assert.equal(button.disabled, true)
      assert.equal(
        button.getAttribute('aria-description'),
        'Archive has been deleted.'
      )
    }

    await unmountActions(rendered)
  })

  test('deduplicates selected rows and clears the batch selection after deletion', async () => {
    let resetCount = 0
    const rows = [
      { original: { raw_exchange: { request_id: 'request-a' } } },
      { original: { raw_exchange: { request_id: 'request-a' } } },
      { original: { raw_exchange: { request_id: 'request-b' } } },
    ]
    const table = {
      getFilteredSelectedRowModel: () => ({ rows }),
      resetRowSelection: () => {
        resetCount += 1
      },
    } as unknown as Table<unknown>
    requestHandler = (config) => {
      if (config.url === '/api/user/2fa/status') {
        return { success: true, data: { enabled: true } }
      }
      if (config.url === '/api/user/passkey') {
        return { success: true, data: { enabled: false } }
      }
      if (config.url === '/api/verify') {
        return { success: true, data: { proof_token: 'batch-proof' } }
      }
      if (config.url === '/api/raw-exchanges/self/batch-delete') {
        assert.deepEqual(JSON.parse(String(config.data)), {
          request_ids: ['request-a', 'request-b'],
        })
        assert.equal(
          (config.headers as AxiosHeaders).get('X-Security-Proof'),
          'batch-proof'
        )
        return {
          success: true,
          data: {
            selected: 2,
            deleted: 2,
            pending: 0,
            already_deleted: 0,
            failed: 0,
            request_bytes_freed: 40,
            response_bytes_freed: 20,
            total_bytes_freed: 60,
          },
        }
      }
      throw new Error(`unexpected request ${config.method} ${config.url}`)
    }
    const rendered = await renderBulkActions(table)

    await click(actionButton('Delete selected raw exchanges'))
    await click(buttonByText('Delete 2 archives'))
    await flushEffects()
    const code = document.querySelector<HTMLInputElement>(
      'input[placeholder="Enter verification code"]'
    )
    assert.ok(code)
    await fill(code, '123456')
    await click(buttonByText('Verify'))
    await flushEffects()

    assert.equal(resetCount, 1)
    await unmountActions(rendered)
  })
})
