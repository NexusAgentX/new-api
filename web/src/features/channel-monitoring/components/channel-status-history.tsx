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
import { ArrowRight, History } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { CHANNEL_STATUS_CONFIG } from '@/features/channels/constants'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { STATUS_TIMELINE_COLORS } from '../constants'
import {
  buildStatusTimelineSegments,
  statusReasonLabelKey,
  statusSourceLabelKey,
} from '../lib/channel-monitoring'
import type { ChannelStatusEvent } from '../types'
import { MonitoringStatusBadge } from './monitoring-status'

export function ChannelStatusHistory(props: {
  events: ChannelStatusEvent[]
  startTs: number
  endTs: number
  currentStatus: number
}) {
  const { t } = useTranslation()
  const segments = useMemo(
    () =>
      buildStatusTimelineSegments(
        props.events,
        props.startTs,
        props.endTs,
        props.currentStatus
      ),
    [props.currentStatus, props.endTs, props.events, props.startTs]
  )

  return (
    <section className='overflow-hidden rounded-lg border'>
      <header className='flex min-h-11 flex-wrap items-center gap-3 border-b px-3 py-2'>
        <History className='text-muted-foreground size-4' aria-hidden='true' />
        <h3 className='text-sm font-semibold'>{t('Channel status history')}</h3>
        <div className='text-muted-foreground ml-auto flex flex-wrap gap-x-3 gap-y-1 text-xs'>
          {Object.entries(CHANNEL_STATUS_CONFIG).map(([status, config]) => (
            <span key={status} className='inline-flex items-center gap-1.5'>
              <span
                className={cn(
                  'size-2 rounded-full',
                  STATUS_TIMELINE_COLORS[Number(status)]
                )}
                aria-hidden='true'
              />
              {t(config.label)}
            </span>
          ))}
        </div>
      </header>

      <div className='border-b px-3 py-4'>
        <div
          className='bg-muted flex h-3 w-full overflow-hidden rounded-sm'
          aria-label={t('Channel status timeline')}
        >
          {segments.map((segment) => (
            <div
              key={`${segment.startTs}-${segment.endTs}-${segment.status}`}
              className={cn(
                'h-full min-w-px',
                STATUS_TIMELINE_COLORS[segment.status]
              )}
              style={{ width: `${segment.widthPercent}%` }}
              title={`${t(
                CHANNEL_STATUS_CONFIG[
                  segment.status as keyof typeof CHANNEL_STATUS_CONFIG
                ]?.label ?? 'Unknown'
              )}: ${formatTimestampToDate(segment.startTs)} - ${formatTimestampToDate(segment.endTs)}`}
            />
          ))}
        </div>
        <div className='text-muted-foreground mt-1.5 flex justify-between text-xs tabular-nums'>
          <span>{formatTimestampToDate(props.startTs)}</span>
          <span>{formatTimestampToDate(props.endTs)}</span>
        </div>
      </div>

      {props.events.length === 0 ? (
        <Empty className='min-h-48'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <History aria-hidden='true' />
            </EmptyMedia>
            <EmptyTitle>{t('No status events')}</EmptyTitle>
            <EmptyDescription>
              {t('No channel status changes occurred in this time range.')}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <Table>
          <TableHeader className='bg-muted/35'>
            <TableRow>
              <TableHead>{t('Time')}</TableHead>
              <TableHead>{t('Scope')}</TableHead>
              <TableHead>{t('Transition')}</TableHead>
              <TableHead>{t('Source and reason')}</TableHead>
              <TableHead>{t('Sanitized detail')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.events.map((event) => (
              <TableRow key={event.id}>
                <TableCell className='align-top'>
                  {formatTimestampToDate(event.created_at)}
                </TableCell>
                <TableCell className='align-top'>
                  {event.scope === 'key' ? (
                    <div className='flex flex-col'>
                      <span>
                        {t('Key {{index}}', { index: event.key_index ?? '-' })}
                      </span>
                      <span className='text-muted-foreground font-mono text-xs'>
                        {event.key_fingerprint}
                      </span>
                    </div>
                  ) : (
                    t('Whole channel')
                  )}
                </TableCell>
                <TableCell className='align-top'>
                  <div className='flex items-center gap-1.5'>
                    <MonitoringStatusBadge status={event.from_status} />
                    <ArrowRight className='size-3.5' aria-hidden='true' />
                    <MonitoringStatusBadge status={event.to_status} />
                  </div>
                </TableCell>
                <TableCell className='max-w-72 align-top whitespace-normal'>
                  <div className='font-medium'>
                    {t(statusSourceLabelKey(event.source))}
                  </div>
                  <div className='text-muted-foreground text-xs'>
                    {t(statusReasonLabelKey(event.reason_code))}
                    {event.reason_code.startsWith('upstream_http_') && (
                      <span className='ml-1 font-mono'>
                        ({event.reason_code})
                      </span>
                    )}
                  </div>
                </TableCell>
                <TableCell className='max-w-md align-top whitespace-normal'>
                  <div>{event.reason_detail || '-'}</div>
                  {event.previous_reason_detail && (
                    <div className='text-muted-foreground mt-1 border-l-2 pl-2 text-xs'>
                      <span className='font-medium'>
                        {t('Previous disable reason')}:
                      </span>{' '}
                      {event.previous_reason_detail}
                    </div>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  )
}
