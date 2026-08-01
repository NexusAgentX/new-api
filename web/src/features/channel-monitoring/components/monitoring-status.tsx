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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import {
  CHANNEL_STATUS_CONFIG,
  CHANNEL_STATUS,
} from '@/features/channels/constants'

import type { RuntimeMode } from '../types'

export function MonitoringStatusBadge(props: { status: number }) {
  const { t } = useTranslation()
  const config =
    CHANNEL_STATUS_CONFIG[props.status as keyof typeof CHANNEL_STATUS_CONFIG] ??
    CHANNEL_STATUS_CONFIG[CHANNEL_STATUS.UNKNOWN]
  let variant: 'default' | 'destructive' | 'outline' | 'warning' = 'outline'
  let className = ''
  if (props.status === CHANNEL_STATUS.ENABLED) {
    variant = 'default'
    className = 'bg-emerald-600 text-white'
  } else if (props.status === CHANNEL_STATUS.MANUAL_DISABLED) {
    variant = 'destructive'
  } else if (props.status === CHANNEL_STATUS.AUTO_DISABLED) {
    variant = 'warning'
  }
  return (
    <Badge variant={variant} className={className}>
      {t(config.label)}
    </Badge>
  )
}

export function RuntimeModeBadge(props: { mode: RuntimeMode }) {
  const { t } = useTranslation()
  let label = t('Process memory')
  let variant: 'default' | 'outline' | 'warning' = 'outline'
  let className = ''
  if (props.mode === 'redis') {
    label = t('Distributed Redis')
    variant = 'default'
    className = 'bg-emerald-600 text-white'
  } else if (props.mode === 'memory_fallback') {
    label = t('Memory fallback')
    variant = 'warning'
  }
  return (
    <Badge variant={variant} className={className}>
      {label}
    </Badge>
  )
}
