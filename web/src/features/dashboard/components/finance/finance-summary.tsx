/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your option)
any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  ArrowDownRight,
  ArrowUpRight,
  CircleDollarSign,
  Landmark,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { IconBadge } from '@/components/ui/icon-badge'
import { cn } from '@/lib/utils'

import type { ChannelFinanceSummary } from '../../types'
import {
  formatChannelFinanceMargin,
  useChannelFinanceMoneyFormatter,
} from './finance-format'

function SummaryMetric(props: {
  label: string
  value: string
  icon: typeof Landmark
  tone: 'info' | 'success' | 'warning' | 'destructive'
  valueClassName?: string
}) {
  const Icon = props.icon
  return (
    <div className='min-w-0 px-3 py-3 sm:px-5 sm:py-4'>
      <div className='flex items-center gap-2'>
        <IconBadge tone={props.tone} size='sm'>
          <Icon aria-hidden='true' />
        </IconBadge>
        <span className='text-muted-foreground truncate text-xs font-medium'>
          {props.label}
        </span>
      </div>
      <div
        className={cn(
          'mt-2 truncate text-lg font-semibold tabular-nums sm:text-xl',
          props.valueClassName
        )}
        title={props.value}
      >
        {props.value}
      </div>
    </div>
  )
}

export function FinanceSummary(props: { summary?: ChannelFinanceSummary }) {
  const { t } = useTranslation()
  const formatMoney = useChannelFinanceMoneyFormatter()
  const summary = props.summary
  const profit = summary?.profit_usd ?? 0
  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='divide-border/60 grid grid-cols-2 divide-x lg:grid-cols-4'>
        <SummaryMetric
          label={t('Billed Revenue')}
          value={formatMoney(summary?.revenue_usd ?? 0)}
          icon={Landmark}
          tone='info'
        />
        <SummaryMetric
          label={t('Channel Cost')}
          value={formatMoney(summary?.cost_usd ?? 0)}
          icon={ArrowDownRight}
          tone='warning'
        />
        <SummaryMetric
          label={t('Gross Profit')}
          value={formatMoney(profit)}
          icon={ArrowUpRight}
          tone={profit >= 0 ? 'success' : 'destructive'}
          valueClassName={
            profit >= 0
              ? 'text-emerald-600 dark:text-emerald-400'
              : 'text-red-600 dark:text-red-400'
          }
        />
        <SummaryMetric
          label={t('Gross Margin')}
          value={formatChannelFinanceMargin(
            summary?.margin ?? 0,
            summary?.revenue_usd ?? 0
          )}
          icon={CircleDollarSign}
          tone='success'
        />
      </div>
    </div>
  )
}
