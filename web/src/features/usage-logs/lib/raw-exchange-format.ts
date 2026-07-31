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
import type { TFunction } from 'i18next'

export function formatRawExchangeBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(
    units.length - 1,
    Math.floor(Math.log(bytes) / Math.log(1024))
  )
  return `${(bytes / 1024 ** index).toFixed(index === 0 ? 0 : 2)} ${units[index]}`
}

export function rawExchangeCaptureModeLabel(
  mode: string,
  t: TFunction
): string {
  switch (mode) {
    case 'all':
      return t('All requests')
    case 'non_success':
      return t('Non-success')
    case 'off':
      return t('Off')
    default:
      return mode || t('Unknown')
  }
}

export function rawExchangeOutcomeLabel(outcome: string, t: TFunction): string {
  switch (outcome) {
    case 'success':
      return t('Success')
    case 'non_success':
      return t('Non-success')
    default:
      return outcome || t('Unknown')
  }
}
