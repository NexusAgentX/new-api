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

import type { ChannelFormValues } from '../../../lib/channel-form'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'HTMLLabelElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
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
const { useForm } = await import('react-hook-form')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { Form } = await import('@/components/ui/form')
const { CHANNEL_FORM_DEFAULT_VALUES } =
  await import('../../../lib/channel-form')
const { ResponsesCompatibilitySettingsFields } =
  await import('../responses-compatibility-settings-fields')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

type HarnessProps = {
  currentType: number
  compatibilityFix: boolean
  allowReasoning: boolean
}

function Harness(props: HarnessProps) {
  const form = useForm<ChannelFormValues>({
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      responses_compatibility_fix: props.compatibilityFix,
      allow_reasoning_without_encrypted_content: props.allowReasoning,
    },
  })
  return (
    <I18nextProvider i18n={i18n}>
      <Form {...form}>
        <ResponsesCompatibilitySettingsFields
          form={form}
          currentType={props.currentType}
          sensitiveLocked={false}
        />
      </Form>
    </I18nextProvider>
  )
}

type Rendered = {
  container: HTMLDivElement
  root: ReturnType<typeof createRoot>
}

async function renderHarness(props: HarnessProps): Promise<Rendered> {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  await act(async () => root.render(<Harness {...props} />))
  return { container, root }
}

async function unmount(rendered: Rendered) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

function getSwitches(container: HTMLDivElement): HTMLButtonElement[] {
  return [...container.querySelectorAll<HTMLButtonElement>('[role="switch"]')]
}

describe('Responses compatibility settings fields', () => {
  after(() => domWindow.close())

  test('shows both switches with the default policy values', async () => {
    const rendered = await renderHarness({
      currentType: 1,
      compatibilityFix: true,
      allowReasoning: false,
    })
    const switches = getSwitches(rendered.container)

    assert.equal(switches.length, 3)
    assert.equal(switches[0].getAttribute('aria-checked'), 'false')
    assert.equal(switches[1].getAttribute('aria-checked'), 'true')
    assert.equal(switches[2].getAttribute('aria-checked'), 'false')
    assert.equal(switches[2].hasAttribute('data-disabled'), false)
    assert.deepEqual(
      [...rendered.container.querySelectorAll('label')].map(
        (label) => label.textContent
      ),
      [
        'Force Format',
        'Responses compatibility fix',
        'Allow reasoning without encrypted content',
      ]
    )
    assert.equal(
      rendered.container.textContent?.includes(
        'Reformat Chat Completions responses (OpenAI channels only)'
      ),
      true
    )
    assert.equal(
      rendered.container.textContent?.includes(
        'Normalize non-standard Item IDs and protect store:false history across channel replay'
      ),
      true
    )

    await unmount(rendered)
  })

  test('disables only the dependent policy when the main fix is off', async () => {
    const rendered = await renderHarness({
      currentType: 1,
      compatibilityFix: false,
      allowReasoning: true,
    })
    const switches = getSwitches(rendered.container)

    assert.equal(switches.length, 3)
    assert.equal(switches[1].getAttribute('aria-checked'), 'false')
    assert.equal(switches[2].getAttribute('aria-checked'), 'true')
    assert.equal(switches[1].hasAttribute('data-disabled'), false)
    assert.equal(switches[2].hasAttribute('data-disabled'), true)

    await unmount(rendered)
  })

  test('does not render for non-OpenAI channels', async () => {
    const rendered = await renderHarness({
      currentType: 14,
      compatibilityFix: true,
      allowReasoning: false,
    })

    assert.equal(getSwitches(rendered.container).length, 0)
    assert.equal(rendered.container.textContent, '')

    await unmount(rendered)
  })
})
