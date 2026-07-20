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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { ChannelFinanceChannel } from '../../types'
import {
  formatChannelFinanceMargin,
  formatChannelFinanceMoney,
} from './finance-format'

function CostModeBadge(props: { channel: ChannelFinanceChannel }) {
  const { t } = useTranslation()
  if (props.channel.cost_mode === 'fixed_daily') {
    return <Badge variant='secondary'>{t('Fixed Daily Cost')}</Badge>
  }
  if (props.channel.cost_mode === 'usage_ratio') {
    return <Badge variant='outline'>{t('Usage Cost Ratio')}</Badge>
  }
  return <Badge variant='outline'>{t('Not configured')}</Badge>
}

export function ChannelFinanceTable(props: {
  channels: ChannelFinanceChannel[]
}) {
  const { t } = useTranslation()
  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='border-b px-4 py-3 sm:px-5'>
        <span className='text-sm font-semibold'>
          {t('Channel Profitability')}
        </span>
      </div>
      <div className='overflow-x-auto'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='min-w-44'>{t('Channel')}</TableHead>
              <TableHead className='min-w-36'>{t('Cost Mode')}</TableHead>
              <TableHead className='text-right'>{t('Requests')}</TableHead>
              <TableHead className='text-right'>{t('Revenue')}</TableHead>
              <TableHead className='text-right'>{t('Variable Cost')}</TableHead>
              <TableHead className='text-right'>{t('Fixed Cost')}</TableHead>
              <TableHead className='text-right'>{t('Total Cost')}</TableHead>
              <TableHead className='text-right'>{t('Gross Profit')}</TableHead>
              <TableHead className='text-right'>{t('Gross Margin')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.channels.map((channel) => (
              <TableRow key={channel.channel_id}>
                <TableCell>
                  <div className='flex items-center gap-2'>
                    <div className='font-medium'>{channel.channel_name}</div>
                    {channel.deleted && (
                      <Badge variant='secondary'>{t('Deleted channel')}</Badge>
                    )}
                  </div>
                  <div className='text-muted-foreground text-xs'>
                    #{channel.channel_id}
                  </div>
                </TableCell>
                <TableCell>
                  <div className='flex items-center gap-2'>
                    <CostModeBadge channel={channel} />
                    {channel.cost_mode === 'fixed_daily' && (
                      <span className='text-muted-foreground text-xs tabular-nums'>
                        {formatChannelFinanceMoney(channel.cost_setting)}/
                        {t('day')}
                      </span>
                    )}
                    {channel.cost_mode === 'usage_ratio' && (
                      <span className='text-muted-foreground text-xs tabular-nums'>
                        {formatNumber(channel.cost_setting)}x
                      </span>
                    )}
                  </div>
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatNumber(channel.request_count)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatChannelFinanceMoney(channel.revenue_usd)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatChannelFinanceMoney(channel.variable_cost_usd)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatChannelFinanceMoney(channel.fixed_cost_usd)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatChannelFinanceMoney(channel.cost_usd)}
                </TableCell>
                <TableCell
                  className={cn(
                    'text-right font-medium tabular-nums',
                    channel.profit_usd >= 0
                      ? 'text-emerald-600 dark:text-emerald-400'
                      : 'text-red-600 dark:text-red-400'
                  )}
                >
                  {formatChannelFinanceMoney(channel.profit_usd)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatChannelFinanceMargin(
                    channel.margin,
                    channel.revenue_usd
                  )}
                </TableCell>
              </TableRow>
            ))}
            {props.channels.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={9}
                  className='text-muted-foreground h-24 text-center'
                >
                  {t('No data')}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
