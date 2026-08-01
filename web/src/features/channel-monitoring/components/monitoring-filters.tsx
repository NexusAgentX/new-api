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
import { RotateCcw } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { ComboboxInput } from '@/components/ui/combobox-input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { CHANNEL_MONITORING_TIME_RANGES } from '../constants'
import type {
  ChannelDimensionResult,
  ChannelMonitoringSearch,
  ChannelRuntimeItem,
} from '../types'

type MonitoringFiltersProps = {
  search: ChannelMonitoringSearch
  channels: ChannelRuntimeItem[]
  dimensions?: ChannelDimensionResult
  onChange: (patch: Partial<ChannelMonitoringSearch>) => void
  onReset: () => void
}

export function MonitoringFilters(props: MonitoringFiltersProps) {
  const { t } = useTranslation()
  const channelOptions = useMemo(
    () => [
      { value: '', label: t('All channels') },
      ...props.channels.map((channel) => ({
        value: String(channel.channel_id),
        label: `#${channel.channel_id} ${channel.channel_name}`,
      })),
    ],
    [props.channels, t]
  )
  const groupOptions = useMemo(
    () => [
      { value: '', label: t('All groups') },
      ...(props.dimensions?.groups ?? []).map((group) => ({
        value: group,
        label: group,
      })),
    ],
    [props.dimensions?.groups, t]
  )
  const modelOptions = useMemo(
    () => [
      { value: '', label: t('All models') },
      ...(props.dimensions?.models ?? []).map((model) => ({
        value: model,
        label: model,
      })),
    ],
    [props.dimensions?.models, t]
  )
  const endpointOptions = useMemo(
    () => [
      { value: '', label: t('All endpoints') },
      ...(props.dimensions?.endpoints ?? []).map((endpoint) => ({
        value: endpoint,
        label: endpoint,
      })),
    ],
    [props.dimensions?.endpoints, t]
  )

  return (
    <div className='bg-muted/20 grid gap-3 border-y px-3 py-3 sm:grid-cols-2 sm:px-4 lg:grid-cols-3 lg:items-end 2xl:grid-cols-[10rem_minmax(13rem,1.2fr)_minmax(10rem,1fr)_minmax(12rem,1.2fr)_minmax(12rem,1.2fr)_auto]'>
      <div className='grid gap-1.5'>
        <Label htmlFor='channel-monitoring-range'>{t('Time range')}</Label>
        <Select
          value={String(props.search.hours)}
          onValueChange={(value) => {
            if (value) props.onChange({ hours: Number(value) })
          }}
        >
          <SelectTrigger id='channel-monitoring-range' className='w-full'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {CHANNEL_MONITORING_TIME_RANGES.map((range) => (
              <SelectItem key={range.value} value={String(range.value)}>
                {t(range.labelKey)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className='grid gap-1.5'>
        <Label htmlFor='channel-monitoring-channel'>{t('Channel')}</Label>
        <ComboboxInput
          id='channel-monitoring-channel'
          options={channelOptions}
          value={props.search.channel ? String(props.search.channel) : ''}
          onValueChange={(value) =>
            props.onChange({ channel: value ? Number(value) : undefined })
          }
          placeholder={t('All channels')}
          emptyText={t('No channels found')}
          className='w-full'
        />
      </div>

      <div className='grid gap-1.5'>
        <Label htmlFor='channel-monitoring-group'>{t('Group')}</Label>
        <ComboboxInput
          id='channel-monitoring-group'
          options={groupOptions}
          value={props.search.group ?? ''}
          onValueChange={(value) =>
            props.onChange({ group: value || undefined })
          }
          placeholder={t('All groups')}
          emptyText={t('No groups found')}
          className='w-full'
        />
      </div>

      <div className='grid gap-1.5'>
        <Label htmlFor='channel-monitoring-model'>{t('Model')}</Label>
        <ComboboxInput
          id='channel-monitoring-model'
          options={modelOptions}
          value={props.search.model ?? ''}
          onValueChange={(value) =>
            props.onChange({ model: value || undefined })
          }
          placeholder={t('All models')}
          emptyText={t('No models found')}
          className='w-full'
        />
      </div>

      <div className='grid gap-1.5'>
        <Label htmlFor='channel-monitoring-endpoint'>{t('Endpoint')}</Label>
        <ComboboxInput
          id='channel-monitoring-endpoint'
          options={endpointOptions}
          value={props.search.endpoint ?? ''}
          onValueChange={(value) =>
            props.onChange({ endpoint: value || undefined })
          }
          placeholder={t('All endpoints')}
          emptyText={t('No endpoints found')}
          className='w-full'
        />
      </div>

      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={props.onReset}
        className='w-full lg:w-auto'
      >
        <RotateCcw aria-hidden='true' />
        {t('Reset filters')}
      </Button>
    </div>
  )
}
