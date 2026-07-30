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

import { isChannelTestEndpointStreamIncompatible } from '../channel-test-options'

describe('channel test endpoint stream compatibility', () => {
  test('disables stream mode only for endpoints without streaming support', () => {
    assert.equal(isChannelTestEndpointStreamIncompatible('embeddings'), true)
    assert.equal(
      isChannelTestEndpointStreamIncompatible('image-generation'),
      true
    )
    assert.equal(isChannelTestEndpointStreamIncompatible('jina-rerank'), true)
    assert.equal(
      isChannelTestEndpointStreamIncompatible('openai-response-compact'),
      true
    )

    assert.equal(isChannelTestEndpointStreamIncompatible('auto'), false)
    assert.equal(isChannelTestEndpointStreamIncompatible('openai'), false)
    assert.equal(
      isChannelTestEndpointStreamIncompatible('openai-response'),
      false
    )
    assert.equal(isChannelTestEndpointStreamIncompatible('anthropic'), false)
    assert.equal(isChannelTestEndpointStreamIncompatible('gemini'), false)
  })
})
