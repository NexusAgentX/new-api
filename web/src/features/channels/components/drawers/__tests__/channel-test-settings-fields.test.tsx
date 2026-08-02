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
import type { ChannelTestEndpointType } from '../../../lib/channel-test-options'

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
const { ChannelTestSettingsFields } =
  await import('../channel-test-settings-fields')

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
  endpointType: ChannelTestEndpointType
  stream: boolean
  sensitiveLocked: boolean
}

function ChannelTestSettingsHarness(props: HarnessProps) {
  const form = useForm<ChannelFormValues>({
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      test_endpoint_type: props.endpointType,
      test_stream: props.stream,
    },
  })
  const streamValue = form.watch('test_stream')

  return (
    <I18nextProvider i18n={i18n}>
      <Form {...form}>
        <ChannelTestSettingsFields
          form={form}
          currentType={1}
          sensitiveLocked={props.sensitiveLocked}
        />
        <output data-testid='stream-value'>{String(streamValue)}</output>
        <output data-testid='dirty-value'>
          {String(form.formState.isDirty)}
        </output>
      </Form>
    </I18nextProvider>
  )
}

type RenderedSettings = {
  container: HTMLDivElement
  root: ReturnType<typeof createRoot>
}

async function renderSettings(
  endpointType: ChannelTestEndpointType,
  stream: boolean,
  sensitiveLocked = false
): Promise<RenderedSettings> {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <ChannelTestSettingsHarness
        endpointType={endpointType}
        stream={stream}
        sensitiveLocked={sensitiveLocked}
      />
    )
  })

  return { container, root }
}

function getStreamControl(container: HTMLDivElement): {
  root: HTMLElement
  input: HTMLInputElement
} {
  const label = [...container.querySelectorAll('label')].find(
    (element) => element.textContent === 'Test stream mode'
  )
  assert.ok(label)
  const formItem = label.closest<HTMLElement>('[data-slot="form-item"]')
  assert.ok(formItem)
  const root = formItem.querySelector<HTMLElement>('[role="switch"]')
  const input = formItem.querySelector<HTMLInputElement>(
    `input[id="${label.htmlFor}"]`
  )
  assert.ok(root)
  assert.ok(input)
  return { root, input }
}

async function unmountSettings(rendered: RenderedSettings) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

describe('channel test settings fields', () => {
  after(() => {
    domWindow.close()
  })

  test('disables stream and normalizes its value for an incompatible endpoint', async () => {
    const rendered = await renderSettings('embeddings', true)
    const streamControl = getStreamControl(rendered.container)

    assert.equal(streamControl.input.disabled, true)
    assert.equal(streamControl.root.getAttribute('aria-checked'), 'false')
    assert.equal(streamControl.root.hasAttribute('data-disabled'), true)
    assert.equal(
      streamControl.root
        .closest('[data-slot="form-item"]')
        ?.hasAttribute('data-disabled'),
      true
    )
    assert.equal(
      rendered.container.querySelector('[data-testid="stream-value"]')
        ?.textContent,
      'false'
    )

    await unmountSettings(rendered)
  })

  test('keeps every test setting editable with sensitive permission', async () => {
    const rendered = await renderSettings('openai-response', true)
    const streamControl = getStreamControl(rendered.container)
    const selectTrigger =
      rendered.container.querySelector<HTMLButtonElement>('[role="combobox"]')
    const numberInputs = [
      ...rendered.container.querySelectorAll<HTMLInputElement>(
        'input[type="number"]'
      ),
    ]
    const switches = [
      ...rendered.container.querySelectorAll<HTMLButtonElement>(
        '[role="switch"]'
      ),
    ]

    assert.ok(selectTrigger)
    assert.equal(selectTrigger.disabled, false)
    assert.equal(numberInputs.length, 2)
    assert.equal(
      numberInputs.every((input) => !input.disabled),
      true
    )
    assert.equal(switches.length, 3)
    assert.equal(
      switches.every((control) => !control.hasAttribute('data-disabled')),
      true
    )
    const replayLabel = [...rendered.container.querySelectorAll('label')].find(
      (element) =>
        element.textContent === 'Replay failure sample before recovery'
    )
    assert.ok(replayLabel)
    const replaySwitch = replayLabel
      .closest<HTMLElement>('[data-slot="form-item"]')
      ?.querySelector<HTMLElement>('[role="switch"]')
    assert.ok(replaySwitch)
    assert.equal(replaySwitch.getAttribute('aria-checked'), 'false')
    assert.equal(streamControl.input.disabled, false)
    assert.equal(streamControl.root.getAttribute('aria-checked'), 'true')
    assert.equal(streamControl.root.hasAttribute('data-disabled'), false)
    assert.equal(
      rendered.container.querySelector('[data-testid="stream-value"]')
        ?.textContent,
      'true'
    )

    await unmountSettings(rendered)
  })

  test('locks every test setting without mutating incompatible stored values', async () => {
    const rendered = await renderSettings('embeddings', true, true)
    const selectTrigger =
      rendered.container.querySelector<HTMLButtonElement>('[role="combobox"]')
    const numberInputs = [
      ...rendered.container.querySelectorAll<HTMLInputElement>(
        'input[type="number"]'
      ),
    ]
    const switches = [
      ...rendered.container.querySelectorAll<HTMLButtonElement>(
        '[role="switch"]'
      ),
    ]

    assert.ok(selectTrigger)
    assert.equal(selectTrigger.disabled, true)
    assert.equal(numberInputs.length, 2)
    assert.equal(
      numberInputs.every((input) => input.disabled),
      true
    )
    assert.equal(switches.length, 3)
    assert.equal(
      switches.every((control) => control.hasAttribute('data-disabled')),
      true
    )
    assert.equal(
      rendered.container.querySelector('[data-testid="stream-value"]')
        ?.textContent,
      'true'
    )
    assert.equal(
      rendered.container.querySelector('[data-testid="dirty-value"]')
        ?.textContent,
      'false'
    )

    await unmountSettings(rendered)
  })
})
