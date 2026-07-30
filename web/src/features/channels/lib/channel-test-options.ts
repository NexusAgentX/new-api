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
export const CHANNEL_TEST_ENDPOINT_TYPES = [
  'auto',
  'openai',
  'openai-response',
  'openai-response-compact',
  'anthropic',
  'gemini',
  'jina-rerank',
  'image-generation',
  'embeddings',
] as const

export type ChannelTestEndpointType =
  (typeof CHANNEL_TEST_ENDPOINT_TYPES)[number]

export const CHANNEL_TEST_ENDPOINT_OPTIONS: ReadonlyArray<{
  value: ChannelTestEndpointType
  label: string
}> = [
  { value: 'auto', label: 'Auto detect (default)' },
  { value: 'openai', label: 'OpenAI (/v1/chat/completions)' },
  { value: 'openai-response', label: 'OpenAI Responses (/v1/responses)' },
  {
    value: 'openai-response-compact',
    label: 'OpenAI Response Compaction (/v1/responses/compact)',
  },
  { value: 'anthropic', label: 'Anthropic (/v1/messages)' },
  {
    value: 'gemini',
    label: 'Gemini (/v1beta/models/{model}:generateContent)',
  },
  { value: 'jina-rerank', label: 'Jina Rerank (/v1/rerank)' },
  {
    value: 'image-generation',
    label: 'Image Generation (/v1/images/generations)',
  },
  { value: 'embeddings', label: 'Embeddings (/v1/embeddings)' },
]

const STREAM_INCOMPATIBLE_CHANNEL_TEST_ENDPOINTS = new Set<string>([
  'embeddings',
  'image-generation',
  'jina-rerank',
  'openai-response-compact',
])

export function isChannelTestEndpointStreamIncompatible(
  endpointType: string
): boolean {
  return STREAM_INCOMPATIBLE_CHANNEL_TEST_ENDPOINTS.has(endpointType)
}
