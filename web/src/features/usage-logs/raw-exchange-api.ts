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
*/
import { api } from '@/lib/api'

export async function downloadRawExchange(
  requestId: string,
  part: 'request' | 'response' | 'bundle',
  proofToken: string
): Promise<{ blob: Blob; filename?: string }> {
  const response = await api.get<Blob>(
    `/api/raw-exchanges/self/${encodeURIComponent(requestId)}/${part}`,
    {
      responseType: 'blob',
      headers: { 'X-Security-Proof': proofToken },
    }
  )
  const contentDisposition = response.headers['content-disposition'] as
    | string
    | undefined
  const filenameMatch = contentDisposition?.match(/filename="?([^";]+)"?/i)
  return {
    blob: response.data,
    filename: filenameMatch?.[1],
  }
}

export async function deleteRawExchange(requestId: string, proofToken: string) {
  const response = await api.delete<{
    success: boolean
    message?: string
    data?: {
      deleted?: boolean
      deletion_pending?: boolean
      already_deleted?: boolean
    }
  }>(`/api/raw-exchanges/self/${encodeURIComponent(requestId)}`, {
    headers: { 'X-Security-Proof': proofToken },
  })
  return response.data
}

export async function deleteRawExchangeBatch(
  requestIds: string[],
  proofToken: string
) {
  const response = await api.post<{
    success: boolean
    message?: string
    data?: {
      selected: number
      deleted: number
      pending: number
      already_deleted: number
      failed: number
      request_bytes_freed: number
      response_bytes_freed: number
      total_bytes_freed: number
    }
  }>(
    '/api/raw-exchanges/self/batch-delete',
    { request_ids: requestIds },
    { headers: { 'X-Security-Proof': proofToken } }
  )
  return response.data
}
