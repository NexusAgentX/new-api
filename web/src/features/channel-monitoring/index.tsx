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
import { Link } from '@tanstack/react-router'
import {
  Database,
  DatabaseZap,
  Radio,
  RefreshCw,
  TriangleAlert,
} from 'lucide-react'
import { useMemo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import {
  getChannelMetricDimensions,
  getChannelMetricsDetail,
  getChannelMetricsOverview,
  getChannelMetricsRuntime,
} from './api'
import { ChannelDetail } from './components/channel-detail'
import { ChannelOverviewTable } from './components/channel-overview-table'
import { MonitoringFilters } from './components/monitoring-filters'
import { buildChannelMonitoringQuery } from './lib/channel-monitoring'
import type { ChannelMonitoringSearch, ChannelRuntimeItem } from './types'

type ChannelMonitoringProps = {
  search: ChannelMonitoringSearch
  onSearchChange: (search: ChannelMonitoringSearch, replace?: boolean) => void
}

const EMPTY_RUNTIME_ITEMS: ChannelRuntimeItem[] = []

export function ChannelMonitoring(props: ChannelMonitoringProps) {
  const { t } = useTranslation()
  const historicalParams = useMemo(
    () => buildChannelMonitoringQuery(props.search),
    [props.search]
  )
  const detailParams = useMemo(
    () => buildChannelMonitoringQuery(props.search, props.search.detail),
    [props.search]
  )
  const dimensionParams = useMemo(
    () => ({
      hours: props.search.hours,
      channel_id: props.search.detail ?? props.search.channel,
    }),
    [props.search.channel, props.search.detail, props.search.hours]
  )
  const runtimeQuery = useQuery({
    queryKey: ['channel-monitoring', 'runtime'],
    queryFn: getChannelMetricsRuntime,
    refetchInterval: 10_000,
    refetchIntervalInBackground: false,
    retry: 1,
  })
  const overviewQuery = useQuery({
    queryKey: ['channel-monitoring', 'overview', historicalParams],
    queryFn: () => getChannelMetricsOverview(historicalParams),
    enabled: !props.search.detail,
    staleTime: 15_000,
  })
  const detailQuery = useQuery({
    queryKey: [
      'channel-monitoring',
      'detail',
      props.search.detail,
      detailParams,
    ],
    queryFn: () =>
      getChannelMetricsDetail(props.search.detail ?? 0, detailParams),
    enabled: Boolean(props.search.detail),
    staleTime: 15_000,
  })
  const dimensionsQuery = useQuery({
    queryKey: ['channel-monitoring', 'dimensions', dimensionParams],
    queryFn: () => getChannelMetricDimensions(dimensionParams),
    staleTime: 60_000,
  })
  const runtimeItems = runtimeQuery.data?.data.items ?? EMPTY_RUNTIME_ITEMS
  const runtimeByChannel = useMemo(
    () =>
      new Map<number, ChannelRuntimeItem>(
        runtimeItems.map((item) => [item.channel_id, item])
      ),
    [runtimeItems]
  )
  const selectedRuntime = props.search.detail
    ? runtimeByChannel.get(props.search.detail)
    : undefined
  const fallbackActive = runtimeItems.some(
    (item) => item.runtime.mode === 'memory_fallback'
  )
  const isRefreshing =
    runtimeQuery.isFetching ||
    overviewQuery.isFetching ||
    detailQuery.isFetching

  const updateSearch = (
    patch: Partial<ChannelMonitoringSearch>,
    replace = true
  ) => {
    const nextSearch = { ...props.search, ...patch }
    if ('channel' in patch && props.search.detail) {
      nextSearch.detail = patch.channel
    }
    props.onSearchChange(nextSearch, replace)
  }
  const resetSearch = () => {
    props.onSearchChange({ hours: 24 }, true)
  }
  const refresh = () => {
    void runtimeQuery.refetch()
    void dimensionsQuery.refetch()
    if (props.search.detail) void detailQuery.refetch()
    else void overviewQuery.refetch()
  }

  const overviewData = overviewQuery.data?.data
  const detailData = detailQuery.data?.data
  const retentionPolicy = detailData ?? overviewData
  const collectionEnabled = detailData?.enabled ?? overviewData?.enabled
  let monitoringContent: ReactNode
  if (props.search.detail) {
    monitoringContent = (
      <ChannelDetail
        data={detailData}
        runtime={selectedRuntime}
        loading={detailQuery.isLoading}
        onBack={() => updateSearch({ detail: undefined })}
        onRetry={() => void detailQuery.refetch()}
      />
    )
  } else if (overviewQuery.isError) {
    monitoringContent = (
      <Alert variant='destructive'>
        <TriangleAlert aria-hidden='true' />
        <AlertTitle>{t('Channel metrics unavailable')}</AlertTitle>
        <AlertDescription>
          {t('The historical channel metrics could not be loaded.')}
        </AlertDescription>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => void overviewQuery.refetch()}
        >
          <RefreshCw aria-hidden='true' />
          {t('Retry')}
        </Button>
      </Alert>
    )
  } else {
    monitoringContent = (
      <ChannelOverviewTable
        items={overviewData?.items ?? []}
        runtimeByChannel={runtimeByChannel}
        loading={overviewQuery.isLoading}
        onOpenChannel={(channelId) =>
          updateSearch({ detail: channelId }, false)
        }
      />
    )
  }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        {t('Channel monitoring')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button variant='outline' size='sm' render={<Link to='/channels' />}>
          <Radio aria-hidden='true' />
          {t('Channels')}
        </Button>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type='button'
                variant='outline'
                size='icon-sm'
                aria-label={t('Refresh channel metrics')}
                onClick={refresh}
              >
                <RefreshCw
                  className={cn(isRefreshing && 'animate-spin')}
                  aria-hidden='true'
                />
              </Button>
            }
          />
          <TooltipContent>{t('Refresh channel metrics')}</TooltipContent>
        </Tooltip>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col overflow-hidden rounded-lg border'>
          <MonitoringFilters
            search={props.search}
            channels={runtimeItems}
            dimensions={dimensionsQuery.data?.data}
            onChange={updateSearch}
            onReset={resetSearch}
          />
          <div className='min-h-0 flex-1 overflow-auto p-3 sm:p-4'>
            <div className='grid gap-3'>
              {retentionPolicy && (
                <div className='text-muted-foreground flex items-start gap-2 px-1 text-xs sm:items-center'>
                  <Database
                    className='mt-0.5 size-3.5 shrink-0 sm:mt-0'
                    aria-hidden='true'
                  />
                  <span>
                    {t(
                      'Metric retention: {{minuteDays}} days of minute buckets and {{hourDays}} days of hourly rollups.',
                      {
                        minuteDays: retentionPolicy.minute_retention_days,
                        hourDays: retentionPolicy.hour_retention_days,
                      }
                    )}
                  </span>
                </div>
              )}
              {runtimeQuery.isError && (
                <Alert variant='destructive'>
                  <TriangleAlert aria-hidden='true' />
                  <AlertTitle>
                    {t('Live channel values unavailable')}
                  </AlertTitle>
                  <AlertDescription>
                    {t(
                      'Historical metrics remain available while live values are retried.'
                    )}
                  </AlertDescription>
                </Alert>
              )}
              {fallbackActive && (
                <Alert>
                  <DatabaseZap aria-hidden='true' />
                  <AlertTitle>
                    {t('Live metrics are using memory fallback')}
                  </AlertTitle>
                  <AlertDescription>
                    {t(
                      'Current RPM and concurrency are process-local until Redis recovers.'
                    )}
                  </AlertDescription>
                </Alert>
              )}
              {collectionEnabled === false && !props.search.detail && (
                <Alert>
                  <DatabaseZap aria-hidden='true' />
                  <AlertTitle>
                    {t('Channel metrics collection is disabled')}
                  </AlertTitle>
                  <AlertDescription>
                    {t(
                      'Previously collected history remains available, but new attempts are not being recorded.'
                    )}
                  </AlertDescription>
                </Alert>
              )}

              {monitoringContent}
            </div>
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
