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

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatQuota } from '@/lib/format'

type UserQuotaCellProps = {
  used: number
  balance: number
  creditQuota: number
  availableQuota: number
  group: string
}

export function UserQuotaCell(props: UserQuotaCellProps) {
  const { t } = useTranslation()
  const usedQuota = Math.max(props.used, 0)
  const creditQuota = Math.max(props.creditQuota, 0)
  const walletBalance = Math.max(props.balance, 0)
  const walletTotal = usedQuota + walletBalance
  const walletUsedPercent =
    walletTotal > 0 ? Math.min((usedQuota / walletTotal) * 100, 100) : 0
  const usedCredit = Math.min(Math.max(-props.balance, 0), creditQuota)
  const creditUsedPercent =
    creditQuota > 0 ? (usedCredit / creditQuota) * 100 : 0

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <div className='w-full min-w-0 cursor-help space-y-1.5 overflow-hidden' />
        }
      >
        <div className='grid min-w-0 grid-cols-2 gap-x-4 text-xs'>
          <div className='min-w-0'>
            <div className='text-muted-foreground truncate'>
              {t('Current Balance')}
            </div>
            <div className='truncate font-medium tabular-nums'>
              {formatQuota(props.balance)}
            </div>
          </div>
          <div className='min-w-0 text-right'>
            <div className='text-muted-foreground truncate'>
              {t('Available to spend')}
            </div>
            <div className='truncate font-medium tabular-nums'>
              {formatQuota(props.availableQuota)}
            </div>
          </div>
        </div>
        <div className='bg-muted relative h-2 overflow-hidden rounded-full'>
          <div
            role='progressbar'
            aria-label={t('Used Quota')}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(walletUsedPercent)}
            className='bg-primary absolute inset-y-0 left-0 rounded-full transition-[width]'
            style={{ width: `${walletUsedPercent}%` }}
          />
          {usedCredit > 0 && (
            <div
              role='progressbar'
              aria-label={t('Using credit')}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={Math.round(creditUsedPercent)}
              className='absolute inset-y-0.5 left-0 rounded-full bg-amber-500 transition-[width]'
              style={{ width: `${creditUsedPercent}%` }}
            />
          )}
        </div>
      </TooltipTrigger>
      <TooltipContent>
        <div className='space-y-1 text-xs'>
          <div>
            {t('Total Usage')}: {formatQuota(usedQuota)}
          </div>
          <div>
            {t('Total Quota')}: {formatQuota(walletTotal)}
          </div>
          <div>
            {t('Current Balance')}: {formatQuota(props.balance)}
          </div>
          <div>
            {t('Credit limit for {{group}}', { group: props.group })}:{' '}
            {formatQuota(creditQuota)}
          </div>
          <div>
            {t('Using credit')}: {formatQuota(usedCredit)}
          </div>
          <div>
            {t('Available to spend')}: {formatQuota(props.availableQuota)}
          </div>
        </div>
      </TooltipContent>
    </Tooltip>
  )
}
