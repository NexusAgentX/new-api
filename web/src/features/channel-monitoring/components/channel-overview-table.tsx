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
import { Activity, ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatNumber, formatPercent } from '@/lib/format'

import {
  formatChannelEventRelativeTime,
  statusReasonLabelKey,
  statusSourceLabelKey,
} from '../lib/channel-monitoring'
import type { ChannelOverviewItem, ChannelRuntimeItem } from '../types'
import { MonitoringStatusBadge, RuntimeModeBadge } from './monitoring-status'

type ChannelOverviewTableProps = {
  items: ChannelOverviewItem[]
  runtimeByChannel: Map<number, ChannelRuntimeItem>
  loading: boolean
  onOpenChannel: (channelId: number) => void
}

function RuntimeValue(props: {
  current?: number
  limit?: number
  runtime?: ChannelRuntimeItem
}) {
  if (!props.runtime || props.current == null) return <span>-</span>
  const limit =
    props.limit && props.limit > 0 ? formatNumber(props.limit) : '\u221e'
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className='cursor-help font-medium tabular-nums'>
            {formatNumber(props.current)} / {limit}
          </span>
        }
      />
      <TooltipContent className='flex items-center gap-2'>
        <RuntimeModeBadge mode={props.runtime.runtime.mode} />
      </TooltipContent>
    </Tooltip>
  )
}

function OverviewLoadingRows() {
  return Array.from({ length: 6 }, (_, index) => (
    <TableRow key={index}>
      <TableCell colSpan={11}>
        <Skeleton className='h-8 w-full' />
      </TableCell>
    </TableRow>
  ))
}

export function ChannelOverviewTable(props: ChannelOverviewTableProps) {
  const { t, i18n } = useTranslation()

  if (!props.loading && props.items.length === 0) {
    return (
      <Empty className='min-h-72 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <Activity aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>{t('No channel metrics')}</EmptyTitle>
          <EmptyDescription>
            {t('No channel attempts match the selected filters.')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='overflow-hidden rounded-lg border'>
      <Table>
        <TableHeader className='bg-muted/35'>
          <TableRow>
            <TableHead>{t('Channel')}</TableHead>
            <TableHead>{t('Status and latest event')}</TableHead>
            <TableHead>{t('Concurrency')}</TableHead>
            <TableHead>{t('RPM')}</TableHead>
            <TableHead>{t('Average TPS')}</TableHead>
            <TableHead>{t('Availability')}</TableHead>
            <TableHead>{t('Error rate')}</TableHead>
            <TableHead>{t('Upstream 429')}</TableHead>
            <TableHead>{t('TTFT P95')}</TableHead>
            <TableHead>{t('First-response intercept rate')}</TableHead>
            <TableHead className='w-10'>
              <span className='sr-only'>{t('Open details')}</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.loading ? (
            <OverviewLoadingRows />
          ) : (
            props.items.map((item) => {
              const runtime = props.runtimeByChannel.get(item.channel_id)
              const latestEvent = item.latest_status_event
              return (
                <TableRow key={item.channel_id}>
                  <TableCell>
                    <button
                      type='button'
                      className='hover:text-primary max-w-64 text-left font-semibold whitespace-normal transition-colors'
                      onClick={() => props.onOpenChannel(item.channel_id)}
                    >
                      <span className='text-muted-foreground mr-1 font-mono text-xs'>
                        #{item.channel_id}
                      </span>
                      {item.channel_name}
                    </button>
                  </TableCell>
                  <TableCell>
                    <div className='flex max-w-72 flex-col items-start gap-1'>
                      <MonitoringStatusBadge status={item.status} />
                      {latestEvent ? (
                        <span className='text-muted-foreground line-clamp-2 text-xs whitespace-normal'>
                          {t(statusSourceLabelKey(latestEvent.source))} ·{' '}
                          {t(statusReasonLabelKey(latestEvent.reason_code))} ·{' '}
                          {formatChannelEventRelativeTime(
                            latestEvent.created_at,
                            i18n.resolvedLanguage || i18n.language
                          )}
                        </span>
                      ) : (
                        <span className='text-muted-foreground text-xs'>
                          {t('No status history')}
                        </span>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    <RuntimeValue
                      current={runtime?.runtime.current_concurrency}
                      limit={runtime?.max_concurrency}
                      runtime={runtime}
                    />
                  </TableCell>
                  <TableCell>
                    <RuntimeValue
                      current={runtime?.runtime.rpm}
                      limit={runtime?.rpm_limit}
                      runtime={runtime}
                    />
                  </TableCell>
                  <TableCell>
                    {item.metrics.average_tps == null
                      ? '-'
                      : formatNumber(item.metrics.average_tps)}
                  </TableCell>
                  <TableCell>
                    {formatPercent(item.metrics.availability_rate)}
                  </TableCell>
                  <TableCell>
                    {formatPercent(item.metrics.error_rate)}
                  </TableCell>
                  <TableCell>
                    {formatNumber(item.metrics.upstream_429_count)}
                  </TableCell>
                  <TableCell>
                    {item.metrics.ttft.p95_ms == null
                      ? '-'
                      : `${formatNumber(item.metrics.ttft.p95_ms)} ms`}
                  </TableCell>
                  <TableCell>
                    {formatPercent(item.metrics.first_response_intercept_rate)}
                  </TableCell>
                  <TableCell>
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon-sm'
                            aria-label={t('Open channel details')}
                            onClick={() => props.onOpenChannel(item.channel_id)}
                          >
                            <ArrowUpRight aria-hidden='true' />
                          </Button>
                        }
                      />
                      <TooltipContent>
                        {t('Open channel details')}
                      </TooltipContent>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              )
            })
          )}
        </TableBody>
      </Table>
    </div>
  )
}
