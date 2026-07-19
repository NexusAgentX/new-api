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
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { getChannelFinance } from '../../api'
import type { ChannelFinanceGranularity } from '../../types'
import { ChannelFinanceTable } from './channel-finance-table'
import { FinanceSummary } from './finance-summary'
import { FinanceTrend } from './finance-trend'

const RANGE_OPTIONS = [7, 30, 90] as const
const GRANULARITY_OPTIONS: Array<{
  value: ChannelFinanceGranularity
  labelKey: string
}> = [
  { value: 'day', labelKey: 'Day' },
  { value: 'week', labelKey: 'Week' },
  { value: 'month', labelKey: 'Month' },
]

function getDateRange(days: number): {
  start_timestamp: number
  end_timestamp: number
} {
  const end = new Date()
  end.setHours(23, 59, 59, 999)
  const start = new Date(end)
  start.setDate(start.getDate() - days + 1)
  start.setHours(0, 0, 0, 0)
  return {
    start_timestamp: Math.floor(start.getTime() / 1_000),
    end_timestamp: Math.floor(end.getTime() / 1_000),
  }
}

export default function ChannelFinanceDashboard() {
  const { t } = useTranslation()
  const [rangeDays, setRangeDays] = useState<number>(30)
  const [granularity, setGranularity] =
    useState<ChannelFinanceGranularity>('day')
  const [showDeletedChannels, setShowDeletedChannels] = useState(false)
  const range = useMemo(() => getDateRange(rangeDays), [rangeDays])
  const timezone = useMemo(
    () => Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
    []
  )
  const query = useQuery({
    queryKey: ['dashboard', 'channel-finance', range, granularity, timezone],
    queryFn: () => getChannelFinance({ ...range, granularity, timezone }),
    select: (response) => response.data,
    staleTime: 30_000,
  })
  const missingCount =
    (query.data?.summary.missing_revenue_count ?? 0) +
    (query.data?.summary.missing_cost_count ?? 0)
  const visibleChannels = useMemo(() => {
    const channels = query.data?.channels ?? []
    if (showDeletedChannels) {
      return channels
    }
    return channels.filter((channel) => !channel.deleted)
  }, [query.data?.channels, showDeletedChannels])

  return (
    <div className='space-y-3 sm:space-y-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <Tabs
          value={String(rangeDays)}
          onValueChange={(value) => setRangeDays(Number(value))}
        >
          <TabsList>
            {RANGE_OPTIONS.map((days) => (
              <TabsTrigger key={days} value={String(days)}>
                {t('{{count}} days', { count: days })}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <Tabs
          value={granularity}
          onValueChange={(value) =>
            setGranularity(value as ChannelFinanceGranularity)
          }
        >
          <TabsList>
            {GRANULARITY_OPTIONS.map((option) => (
              <TabsTrigger key={option.value} value={option.value}>
                {t(option.labelKey)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <div className='flex items-center gap-2 sm:ml-auto'>
          <Switch
            id='show-deleted-channels'
            checked={showDeletedChannels}
            onCheckedChange={setShowDeletedChannels}
          />
          <Label htmlFor='show-deleted-channels' className='cursor-pointer'>
            {t('Show deleted channels')}
          </Label>
        </div>
      </div>

      {missingCount > 0 && (
        <Alert>
          <AlertTriangle aria-hidden='true' />
          <AlertDescription>
            {t(
              'Some requests in this period do not have channel finance data. Cost and profit may be understated.'
            )}
          </AlertDescription>
        </Alert>
      )}

      {query.isError && (
        <Alert variant='destructive'>
          <AlertTriangle aria-hidden='true' />
          <AlertDescription>{t('Failed to load')}</AlertDescription>
        </Alert>
      )}
      {query.isLoading && (
        <div className='space-y-3'>
          <Skeleton className='h-28 w-full rounded-lg' />
          <Skeleton className='h-96 w-full rounded-lg' />
          <Skeleton className='h-72 w-full rounded-lg' />
        </div>
      )}
      {!query.isError && !query.isLoading && (
        <>
          <FinanceSummary summary={query.data?.summary} />
          <FinanceTrend
            summary={query.data?.summary}
            periods={query.data?.periods ?? []}
            channels={visibleChannels}
            granularity={granularity}
          />
          <ChannelFinanceTable channels={visibleChannels} />
        </>
      )}
    </div>
  )
}
