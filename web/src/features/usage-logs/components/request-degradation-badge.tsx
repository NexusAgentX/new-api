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
import { AlertTriangle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { getRequestDegradation } from '../lib/format'
import type { LogOtherData } from '../types'

interface RequestDegradationBadgeProps {
  other: LogOtherData | null
}

export function RequestDegradationBadge(props: RequestDegradationBadgeProps) {
  const { t } = useTranslation()
  const degradation = getRequestDegradation(props.other)
  if (!degradation) return null

  const description = t('Removed non-replayable reasoning items: {{count}}', {
    count: degradation.dropped_reasoning_items,
  })

  return (
    <TooltipProvider delay={300}>
      <Tooltip>
        <TooltipTrigger
          render={
            <span
              data-request-degradation-badge='true'
              className='inline-flex max-w-full'
              tabIndex={0}
              aria-label={`${t('Degraded')}: ${description}`}
            />
          }
        >
          <StatusBadge
            label={t('Degraded')}
            icon={AlertTriangle}
            variant='warning'
            size='sm'
            copyable={false}
            className='text-xs'
          />
        </TooltipTrigger>
        <TooltipContent side='top' className='max-w-xs'>
          {description}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
