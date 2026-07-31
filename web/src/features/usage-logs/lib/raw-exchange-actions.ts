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
import type { RawExchangeSummary } from '../data/schema'

export type RawExchangeActionAvailability = {
  enabled: boolean
  reason?: string
}

export type RawExchangeActionState = {
  request: RawExchangeActionAvailability
  response: RawExchangeActionAvailability
  bundle: RawExchangeActionAvailability
  delete: RawExchangeActionAvailability
}

function archiveUnavailableReason(status: string): string {
  switch (status) {
    case 'pending_commit':
      return 'Archive is still being stored.'
    case 'deleted':
      return 'Archive has been deleted.'
    case 'missing':
      return 'Archived data is missing.'
    case 'delete_pending':
      return 'Archive deletion is already in progress.'
    default:
      return 'Archived data is unavailable.'
  }
}

export function getRawExchangeActionState(
  archive: RawExchangeSummary
): RawExchangeActionState {
  const stored = archive.status === 'stored'
  const archiveReason = archiveUnavailableReason(archive.status)
  const request =
    stored && archive.request_available
      ? { enabled: true }
      : {
          enabled: false,
          reason: stored ? 'Archived request is unavailable.' : archiveReason,
        }
  const response =
    stored && archive.response_available
      ? { enabled: true }
      : {
          enabled: false,
          reason: stored ? 'No response was archived.' : archiveReason,
        }
  const deleteState =
    archive.status === 'deleted' || archive.status === 'delete_pending'
      ? { enabled: false, reason: archiveReason }
      : { enabled: true }

  return {
    request,
    response,
    bundle: request,
    delete: deleteState,
  }
}
