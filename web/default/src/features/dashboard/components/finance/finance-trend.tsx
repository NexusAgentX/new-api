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
import { VChart } from '@visactor/react-vchart'
import { CircleDollarSign } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { IconBadge } from '@/components/ui/icon-badge'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useTheme } from '@/context/theme-provider'
import dayjs from '@/lib/dayjs'
import { formatNumber } from '@/lib/format'
import { VCHART_OPTION } from '@/lib/vchart'

import type {
  ChannelFinanceChannel,
  ChannelFinanceGranularity,
  ChannelFinancePeriod,
  ChannelFinanceSummary,
} from '../../types'
import { formatChannelFinanceMoney } from './finance-format'

export function FinanceTrend(props: {
  summary?: ChannelFinanceSummary
  periods: ChannelFinancePeriod[]
  channels: ChannelFinanceChannel[]
  granularity: ChannelFinanceGranularity
}) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const [selectedChannelID, setSelectedChannelID] = useState('all')
  const selectedChannel = props.channels.find(
    (channel) => String(channel.channel_id) === selectedChannelID
  )
  const visiblePeriods = selectedChannel?.periods ?? props.periods
  const visibleSummary = selectedChannel ?? props.summary
  const values = useMemo(() => {
    const result: Array<{ period: string; metric: string; value: number }> = []
    for (const period of visiblePeriods) {
      let label = dayjs.unix(period.period_start).format('MM-DD')
      if (props.granularity === 'month') {
        label = dayjs.unix(period.period_start).format('YYYY-MM')
      }
      result.push(
        { period: label, metric: t('Revenue'), value: period.revenue_usd },
        { period: label, metric: t('Cost'), value: period.cost_usd },
        { period: label, metric: t('Profit'), value: period.profit_usd }
      )
    }
    return result
  }, [props.granularity, t, visiblePeriods])

  const spec = useMemo(
    () => ({
      type: 'line',
      data: [{ id: 'financeTrend', values }],
      xField: 'period',
      yField: 'value',
      seriesField: 'metric',
      point: { visible: true },
      legends: { visible: true, orient: 'top' },
      axes: [
        { orient: 'bottom', label: { autoRotate: true, autoHide: true } },
        {
          orient: 'left',
          label: {
            formatMethod: (value: number) => formatChannelFinanceMoney(value),
          },
        },
      ],
      tooltip: {
        mark: {
          content: [
            {
              key: (datum: { metric: string }) => datum.metric,
              value: (datum: { value: number }) =>
                formatChannelFinanceMoney(datum.value),
            },
          ],
        },
      },
      padding: { left: 16, right: 16, top: 8, bottom: 8 },
      theme: resolvedTheme === 'dark' ? 'dark' : 'light',
      background: 'transparent',
    }),
    [resolvedTheme, values]
  )

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex flex-wrap items-center gap-2 border-b px-4 py-3 sm:px-5'>
        <IconBadge tone='info' size='sm'>
          <CircleDollarSign aria-hidden='true' />
        </IconBadge>
        <span className='text-sm font-semibold'>{t('Finance Trend')}</span>
        <span className='text-muted-foreground text-xs'>
          {formatNumber(visibleSummary?.request_count ?? 0)} {t('requests')}
        </span>
        <Select
          value={selectedChannelID}
          onValueChange={(value) => setSelectedChannelID(value ?? 'all')}
        >
          <SelectTrigger className='ml-auto h-8 w-full sm:w-56'>
            <SelectValue>
              {selectedChannel?.channel_name ?? t('All Channels')}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='all'>{t('All Channels')}</SelectItem>
            {props.channels.map((channel) => (
              <SelectItem
                key={channel.channel_id}
                value={String(channel.channel_id)}
              >
                {channel.channel_name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className='h-72 p-2 sm:h-96'>
        {values.length > 0 ? (
          <VChart spec={spec} option={VCHART_OPTION} />
        ) : (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {t('No data')}
          </div>
        )}
      </div>
    </div>
  )
}
