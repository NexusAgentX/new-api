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
import { VChart } from '@visactor/react-vchart'
import { Activity, Gauge, Timer, Zap } from 'lucide-react'
import { useMemo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { useTheme } from '@/context/theme-provider'
import dayjs from '@/lib/dayjs'
import { formatNumber, formatPercent } from '@/lib/format'
import { VCHART_OPTION } from '@/lib/vchart'

import type { ChannelMetricSeriesPoint } from '../types'

type ChartValue = {
  time: string
  metric: string
  value: number
}

type MonitoringLineChartProps = {
  title: string
  icon: ReactNode
  values: ChartValue[]
  colors: string[]
  valueFormatter: (value: number) => string
}

function MonitoringLineChart(props: MonitoringLineChartProps) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const spec = {
    type: 'line',
    data: [{ id: 'channelMetric', values: props.values }],
    xField: 'time',
    yField: 'value',
    seriesField: 'metric',
    color: props.colors,
    point: { visible: props.values.length <= 120, size: 4 },
    line: { curveType: 'monotone' },
    legends: { visible: true, orient: 'top' },
    axes: [
      {
        orient: 'bottom',
        label: { autoHide: true, autoRotate: false },
      },
      {
        orient: 'left',
        label: {
          formatMethod: (value: number) => props.valueFormatter(value),
        },
      },
    ],
    tooltip: {
      dimension: {
        content: [
          {
            key: (datum: ChartValue) => datum.metric,
            value: (datum: ChartValue) => props.valueFormatter(datum.value),
          },
        ],
      },
    },
    padding: { left: 12, right: 12, top: 8, bottom: 8 },
    theme: resolvedTheme === 'dark' ? 'dark' : 'light',
    background: 'transparent',
  }

  return (
    <section className='overflow-hidden rounded-lg border'>
      <header className='flex h-11 items-center gap-2 border-b px-3'>
        <span className='text-muted-foreground [&_svg]:size-4'>
          {props.icon}
        </span>
        <h3 className='text-sm font-semibold'>{props.title}</h3>
      </header>
      <div className='h-72 p-2'>
        {props.values.length > 0 ? (
          <VChart spec={spec} option={VCHART_OPTION} />
        ) : (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {t('No samples in this time range')}
          </div>
        )}
      </div>
    </section>
  )
}

function chartTimeLabel(point: ChannelMetricSeriesPoint): string {
  const value = dayjs.unix(point.ts)
  if (point.bucket_seconds >= 3600) return value.format('MM-DD HH:mm')
  return value.format('HH:mm')
}

export function ChannelMetricCharts(props: {
  series: ChannelMetricSeriesPoint[]
}) {
  const { t } = useTranslation()
  const ttftValues = useMemo(() => {
    const values: ChartValue[] = []
    for (const point of props.series) {
      const time = chartTimeLabel(point)
      if (point.metrics.ttft.p50_ms != null) {
        values.push({
          time,
          metric: t('P50'),
          value: point.metrics.ttft.p50_ms,
        })
      }
      if (point.metrics.ttft.p95_ms != null) {
        values.push({
          time,
          metric: t('P95'),
          value: point.metrics.ttft.p95_ms,
        })
      }
      if (point.metrics.ttft.p99_ms != null) {
        values.push({
          time,
          metric: t('P99'),
          value: point.metrics.ttft.p99_ms,
        })
      }
    }
    return values
  }, [props.series, t])
  const tpsValues = useMemo(
    () =>
      props.series.flatMap((point) =>
        point.metrics.average_tps == null
          ? []
          : [
              {
                time: chartTimeLabel(point),
                metric: t('Average TPS'),
                value: point.metrics.average_tps,
              },
            ]
      ),
    [props.series, t]
  )
  const qualityValues = useMemo(
    () =>
      props.series.flatMap((point) => [
        {
          time: chartTimeLabel(point),
          metric: t('Availability'),
          value: point.metrics.availability_rate,
        },
        {
          time: chartTimeLabel(point),
          metric: t('Error rate'),
          value: point.metrics.error_rate,
        },
      ]),
    [props.series, t]
  )
  const trafficValues = useMemo(
    () =>
      props.series.flatMap((point) => [
        {
          time: chartTimeLabel(point),
          metric: t('RPM'),
          value: point.metrics.rpm,
        },
        {
          time: chartTimeLabel(point),
          metric: t('Peak concurrency'),
          value: point.peak_concurrency,
        },
      ]),
    [props.series, t]
  )

  return (
    <div className='grid gap-3 xl:grid-cols-2'>
      <MonitoringLineChart
        title={t('TTFT percentiles')}
        icon={<Timer aria-hidden='true' />}
        values={ttftValues}
        colors={['#2563eb', '#0891b2', '#d97706']}
        valueFormatter={(value) => `${formatNumber(value)} ms`}
      />
      <MonitoringLineChart
        title={t('Output token throughput')}
        icon={<Zap aria-hidden='true' />}
        values={tpsValues}
        colors={['#059669']}
        valueFormatter={formatNumber}
      />
      <MonitoringLineChart
        title={t('Attempt quality')}
        icon={<Activity aria-hidden='true' />}
        values={qualityValues}
        colors={['#059669', '#e11d48']}
        valueFormatter={formatPercent}
      />
      <MonitoringLineChart
        title={t('Traffic and concurrency')}
        icon={<Gauge aria-hidden='true' />}
        values={trafficValues}
        colors={['#7c3aed', '#d97706']}
        valueFormatter={formatNumber}
      />
    </div>
  )
}
