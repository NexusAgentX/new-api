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

import { serializeModelMappingRows } from './model-mapping-serialization'

describe('model mapping row serialization', () => {
  test('keeps a target-first draft out of the controlled form value', () => {
    assert.equal(serializeModelMappingRows([{ from: '', to: 'glm-5.2' }]), '')
  })

  test('serializes a complete mapping', () => {
    assert.equal(
      serializeModelMappingRows([
        { from: ' client-model ', to: ' gateway-model ' },
      ]),
      JSON.stringify({ 'client-model': 'gateway-model' }, null, 2)
    )
  })

  test('does not change existing mappings when a target-first draft is added', () => {
    assert.equal(
      serializeModelMappingRows([
        { from: 'client-model', to: 'gateway-model' },
        { from: '', to: 'glm-5.2' },
      ]),
      JSON.stringify({ 'client-model': 'gateway-model' }, null, 2)
    )
  })
})
