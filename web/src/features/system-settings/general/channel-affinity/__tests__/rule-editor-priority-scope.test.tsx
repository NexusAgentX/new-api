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

import type { AffinityRule } from '../types'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLFormElement',
  'HTMLInputElement',
  'HTMLLabelElement',
  'HTMLTextAreaElement',
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
const { RuleEditorDialog } = await import('../rule-editor-dialog')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

type RenderedDialog = {
  root: ReturnType<typeof createRoot>
  container: HTMLDivElement
  savedRules: AffinityRule[]
}

async function renderRuleEditor(
  rule: AffinityRule | null
): Promise<RenderedDialog> {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const savedRules: AffinityRule[] = []

  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <RuleEditorDialog
          open
          onOpenChange={() => undefined}
          rule={rule}
          onSave={(savedRule) => savedRules.push(savedRule)}
        />
      </I18nextProvider>
    )
  })

  return { root, container, savedRules }
}

function getDialogContent(): HTMLElement {
  const content = document.querySelector<HTMLElement>(
    '[data-slot="dialog-content"]'
  )
  assert.ok(content)
  return content
}

async function getPrioritySwitch(): Promise<HTMLElement> {
  const content = getDialogContent()
  const advancedButton = [...content.querySelectorAll('button')].find(
    (button) => button.textContent?.includes('Advanced Settings')
  )
  assert.ok(advancedButton)
  await act(async () => advancedButton.click())

  const label = [...content.querySelectorAll('label')].find(
    (element) => element.textContent === 'Include Priority'
  )
  assert.ok(label)

  let row: HTMLElement | null = label.parentElement
  while (row && !row.querySelector('[role="switch"]')) {
    row = row.parentElement
  }
  assert.ok(row)
  const control = row.querySelector<HTMLElement>('[role="switch"]')
  assert.ok(control)
  return control
}

async function unmountRuleEditor(rendered: RenderedDialog) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

describe('channel affinity priority scope editor', () => {
  after(() => {
    domWindow.close()
  })

  test('enables priority scope for a new blank rule', async () => {
    const rendered = await renderRuleEditor(null)
    const prioritySwitch = await getPrioritySwitch()

    assert.equal(prioritySwitch.getAttribute('aria-checked'), 'true')

    await unmountRuleEditor(rendered)
  })

  test('keeps a legacy rule without include_priority unscoped when saved', async () => {
    const legacyRule = {
      name: 'legacy-affinity',
      model_regex: ['^gpt-test$'],
      path_regex: ['/v1/responses'],
      key_sources: [{ type: 'request_header', key: 'X-Affinity-Key' }],
      value_regex: '',
      ttl_seconds: 0,
      skip_retry_on_failure: false,
      include_using_group: true,
      include_model_name: false,
      include_rule_name: true,
    } as AffinityRule
    const rendered = await renderRuleEditor(legacyRule)
    const prioritySwitch = await getPrioritySwitch()

    assert.equal(prioritySwitch.getAttribute('aria-checked'), 'false')

    const saveButton = [...getDialogContent().querySelectorAll('button')].find(
      (button) => button.textContent === 'Save'
    )
    assert.ok(saveButton)
    await act(async () => {
      saveButton.click()
      await Promise.resolve()
    })

    assert.equal(rendered.savedRules.length, 1)
    assert.equal(rendered.savedRules[0]?.include_priority, false)

    await unmountRuleEditor(rendered)
  })
})
