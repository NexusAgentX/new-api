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
import { ArrowLeft, DatabaseZap, RefreshCw, TriangleAlert } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Progress, ProgressLabel } from '@/components/ui/progress'
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

import type {
  ChannelDetailResult,
  ChannelErrorBreakdownItem,
  ChannelMetricSummary,
  ChannelRuntimeItem,
} from '../types'
import { ChannelMetricCharts } from './channel-metric-charts'
import { ChannelStatusHistory } from './channel-status-history'
import { MonitoringStatusBadge, RuntimeModeBadge } from './monitoring-status'

type BreakdownItem = {
  label: string
  value: number
}

function MetricStrip(props: { summary: ChannelMetricSummary }) {
  const { t } = useTranslation()
  const values = [
    {
      label: t('Upstream attempts'),
      value: formatNumber(props.summary.attempt_count),
    },
    {
      label: t('Availability'),
      value: formatPercent(props.summary.availability_rate),
    },
    { label: t('Error rate'), value: formatPercent(props.summary.error_rate) },
    {
      label: t('Average TPS'),
      value:
        props.summary.average_tps == null
          ? '-'
          : formatNumber(props.summary.average_tps),
    },
    {
      label: t('TTFT P95'),
      value:
        props.summary.ttft.p95_ms == null
          ? '-'
          : `${formatNumber(props.summary.ttft.p95_ms)} ms`,
    },
    {
      label: t('First-response intercept rate'),
      value: formatPercent(props.summary.first_response_intercept_rate),
    },
    {
      label: t('Peak concurrency'),
      value: formatNumber(props.summary.peak_concurrency),
    },
    { label: t('Retries'), value: formatNumber(props.summary.retry_count) },
  ]
  return (
    <dl className='grid overflow-hidden rounded-lg border sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-8'>
      {values.map((metric) => (
        <div
          key={metric.label}
          className='min-w-0 border-b px-3 py-3 last:border-b-0 sm:nth-[2n+1]:border-r sm:nth-last-[-n+2]:border-b-0 lg:border-r lg:nth-[4n]:border-r-0 lg:nth-last-[-n+4]:border-b-0 xl:border-b-0 xl:last:border-r-0 xl:nth-[4n]:border-r'
        >
          <dt className='text-muted-foreground truncate text-xs'>
            {metric.label}
          </dt>
          <dd className='mt-1 text-base font-semibold tabular-nums'>
            {metric.value}
          </dd>
        </div>
      ))}
    </dl>
  )
}

function BreakdownPanel(props: {
  title: string
  items: BreakdownItem[]
  icon: ReactNode
}) {
  const maxValue = Math.max(1, ...props.items.map((item) => item.value))
  return (
    <section className='overflow-hidden rounded-lg border'>
      <header className='flex h-11 items-center gap-2 border-b px-3'>
        <span className='text-muted-foreground [&_svg]:size-4'>
          {props.icon}
        </span>
        <h3 className='text-sm font-semibold'>{props.title}</h3>
      </header>
      <div className='grid gap-3 p-3'>
        {props.items.map((item) => (
          <Progress
            key={item.label}
            value={(item.value / maxValue) * 100}
            aria-label={item.label}
          >
            <ProgressLabel>{item.label}</ProgressLabel>
            <span className='text-muted-foreground ml-auto text-sm tabular-nums'>
              {formatNumber(item.value)}
            </span>
          </Progress>
        ))}
      </div>
    </section>
  )
}

function ErrorCodeBreakdown(props: { items: ChannelErrorBreakdownItem[] }) {
  const { t } = useTranslation()
  return (
    <section className='overflow-hidden rounded-lg border'>
      <header className='flex h-11 items-center gap-2 border-b px-3'>
        <TriangleAlert
          className='text-muted-foreground size-4'
          aria-hidden='true'
        />
        <h3 className='text-sm font-semibold'>{t('Error codes')}</h3>
      </header>
      {props.items.length === 0 ? (
        <div className='text-muted-foreground flex min-h-24 items-center justify-center text-sm'>
          {t('No error codes in this time range')}
        </div>
      ) : (
        <Table>
          <TableHeader className='bg-muted/35'>
            <TableRow>
              <TableHead>{t('HTTP status')}</TableHead>
              <TableHead>{t('Error code')}</TableHead>
              <TableHead className='text-right'>
                {t('Failed attempts')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.items.map((item) => (
              <TableRow key={`${item.http_status}-${item.error_code}`}>
                <TableCell className='font-mono'>
                  {item.http_status > 0 ? item.http_status : '-'}
                </TableCell>
                <TableCell className='font-mono'>{item.error_code}</TableCell>
                <TableCell className='text-right font-medium'>
                  {formatNumber(item.count)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  )
}

function ChannelDetailLoading() {
  return (
    <div className='grid gap-3'>
      <Skeleton className='h-16 w-full' />
      <Skeleton className='h-28 w-full' />
      <div className='grid gap-3 xl:grid-cols-2'>
        <Skeleton className='h-80 w-full' />
        <Skeleton className='h-80 w-full' />
      </div>
    </div>
  )
}

export function ChannelDetail(props: {
  data?: ChannelDetailResult
  runtime?: ChannelRuntimeItem
  loading: boolean
  onBack: () => void
  onRetry: () => void
}) {
  const { t } = useTranslation()
  if (props.loading) return <ChannelDetailLoading />
  if (!props.data) {
    return (
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
          onClick={props.onRetry}
        >
          <RefreshCw aria-hidden='true' />
          {t('Retry')}
        </Button>
      </Alert>
    )
  }

  const errorItems: BreakdownItem[] = [
    { label: t('Upstream 429'), value: props.data.summary.upstream_429_count },
    { label: t('Upstream 4xx'), value: props.data.summary.upstream_4xx_count },
    { label: t('Upstream 5xx'), value: props.data.summary.upstream_5xx_count },
    {
      label: t('Upstream timeout'),
      value: props.data.summary.upstream_timeout_count,
    },
    { label: t('Client canceled'), value: props.data.summary.canceled_count },
    { label: t('Stream error'), value: props.data.summary.stream_error_count },
    { label: t('Other error'), value: props.data.summary.other_error_count },
  ]
  const skipItems: BreakdownItem[] = [
    {
      label: t('Concurrency limit skips'),
      value: props.data.summary.concurrency_rejected_count,
    },
    {
      label: t('RPM limit skips'),
      value: props.data.summary.rpm_rejected_count,
    },
    {
      label: t('Disabled channel skips'),
      value: props.data.summary.disabled_skip_count,
    },
    {
      label: t('Cooldown skips'),
      value: props.data.summary.cooldown_skip_count,
    },
  ]
  const concurrencyLimit =
    props.runtime && props.runtime.max_concurrency > 0
      ? formatNumber(props.runtime.max_concurrency)
      : '\u221e'
  const rpmLimit =
    props.runtime && props.runtime.rpm_limit > 0
      ? formatNumber(props.runtime.rpm_limit)
      : '\u221e'

  return (
    <div className='grid gap-3'>
      <div className='flex flex-wrap items-center gap-2 border-b pb-3'>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                aria-label={t('Back to channel overview')}
                onClick={props.onBack}
              >
                <ArrowLeft aria-hidden='true' />
              </Button>
            }
          />
          <TooltipContent>{t('Back to channel overview')}</TooltipContent>
        </Tooltip>
        <div className='min-w-0'>
          <h3 className='truncate text-base font-semibold'>
            <span className='text-muted-foreground mr-1 font-mono text-xs'>
              #{props.data.channel_id}
            </span>
            {props.data.channel_name}
          </h3>
        </div>
        <MonitoringStatusBadge status={props.data.status} />
        {props.runtime && (
          <div className='ml-auto flex flex-wrap items-center gap-2 text-xs tabular-nums'>
            <RuntimeModeBadge mode={props.runtime.runtime.mode} />
            <span className='rounded-md border px-2 py-1'>
              {t('Concurrency')}:{' '}
              {formatNumber(props.runtime.runtime.current_concurrency)} /{' '}
              {concurrencyLimit}
            </span>
            <span className='rounded-md border px-2 py-1'>
              {t('RPM')}: {formatNumber(props.runtime.runtime.rpm)} / {rpmLimit}
            </span>
          </div>
        )}
      </div>

      {!props.data.enabled && (
        <Alert>
          <DatabaseZap aria-hidden='true' />
          <AlertTitle>{t('Channel metrics collection is disabled')}</AlertTitle>
          <AlertDescription>
            {t(
              'Previously collected history remains available, but new attempts are not being recorded.'
            )}
          </AlertDescription>
        </Alert>
      )}

      <MetricStrip summary={props.data.summary} />
      <ChannelMetricCharts series={props.data.series} />
      <div className='grid gap-3 xl:grid-cols-2'>
        <BreakdownPanel
          title={t('Upstream error breakdown')}
          icon={<TriangleAlert aria-hidden='true' />}
          items={errorItems}
        />
        <BreakdownPanel
          title={t('Local routing skips')}
          icon={<DatabaseZap aria-hidden='true' />}
          items={skipItems}
        />
      </div>
      <ErrorCodeBreakdown items={props.data.error_breakdown} />
      <ChannelStatusHistory
        events={props.data.status_events}
        startTs={props.data.start_ts}
        endTs={props.data.end_ts}
        currentStatus={props.data.status}
      />
    </div>
  )
}
