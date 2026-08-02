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
import { useEffect, useMemo } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'

import {
  CHANNEL_TEST_ENDPOINT_OPTIONS,
  isChannelTestEndpointStreamIncompatible,
  type ChannelFormValues,
} from '../../lib'

type ChannelTestSettingsFieldsProps = {
  form: UseFormReturn<ChannelFormValues>
  currentType: number
  sensitiveLocked: boolean
}

export function ChannelTestSettingsFields(
  props: ChannelTestSettingsFieldsProps
) {
  const { t } = useTranslation()
  const currentEndpointType = props.form.watch('test_endpoint_type')
  const currentStream = props.form.watch('test_stream')
  const endpointOptions = useMemo(
    () =>
      CHANNEL_TEST_ENDPOINT_OPTIONS.map((option) => ({
        value: option.value,
        label: t(option.label),
      })),
    [t]
  )
  const streamDisabled = isChannelTestEndpointStreamIncompatible(
    currentEndpointType || 'auto'
  )

  useEffect(() => {
    if (props.sensitiveLocked || !streamDisabled || currentStream !== true) {
      return
    }
    props.form.setValue('test_stream', false, { shouldDirty: false })
  }, [currentStream, props.form, props.sensitiveLocked, streamDisabled])

  return (
    <>
      <div className='grid gap-4 sm:grid-cols-2'>
        <FormField
          control={props.form.control}
          name='test_endpoint_type'
          render={({ field }) => (
            <FormItem data-disabled={props.sensitiveLocked || undefined}>
              <FormLabel>{t('Test endpoint type')}</FormLabel>
              <Select
                disabled={props.sensitiveLocked}
                items={endpointOptions}
                onValueChange={(value) => {
                  if (value === null) return
                  field.onChange(value)
                  if (isChannelTestEndpointStreamIncompatible(value)) {
                    props.form.setValue('test_stream', false, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                }}
                value={field.value || 'auto'}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent
                  alignItemWithTrigger={false}
                  className='w-[460px] max-w-[calc(100vw-2rem)]'
                >
                  <SelectGroup>
                    {endpointOptions.map((option) => (
                      <SelectItem
                        key={option.value}
                        value={option.value}
                        className='items-start py-2 [&_[data-slot=select-item-text]]:min-w-0 [&_[data-slot=select-item-text]]:shrink [&_[data-slot=select-item-text]]:whitespace-normal'
                      >
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FormDescription>
                {t(
                  'Select the request endpoint used by automatic channel tests.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={props.form.control}
          name='test_sample_tokens'
          render={({ field }) => (
            <FormItem data-disabled={props.sensitiveLocked || undefined}>
              <FormLabel>{t('Test sample size (tokens)')}</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={0}
                  max={8192}
                  step={1}
                  inputMode='numeric'
                  disabled={props.sensitiveLocked}
                  value={field.value ?? 0}
                  onChange={(event) => {
                    const value = event.target.valueAsNumber
                    field.onChange(Number.isNaN(value) ? 0 : value)
                  }}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Approximate input tokens generated for each automatic channel test.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>

      <div className='grid gap-4 sm:grid-cols-2'>
        <FormField
          control={props.form.control}
          name='test_stream'
          render={({ field }) => (
            <FormItem
              className='flex items-center justify-between'
              data-disabled={
                props.sensitiveLocked || streamDisabled || undefined
              }
            >
              <div className='space-y-0.5'>
                <FormLabel>{t('Test stream mode')}</FormLabel>
                <FormDescription>
                  {streamDisabled
                    ? t('This endpoint does not support streaming.')
                    : t(
                        'Use streaming for automatic channel tests when the endpoint supports it.'
                      )}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={
                    !streamDisabled &&
                    (field.value === true ||
                      (field.value === undefined && props.currentType === 57))
                  }
                  disabled={props.sensitiveLocked || streamDisabled}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={props.form.control}
          name='test_prepend_nonce'
          render={({ field }) => (
            <FormItem
              className='flex items-center justify-between'
              data-disabled={props.sensitiveLocked || undefined}
            >
              <div className='space-y-0.5'>
                <FormLabel>{t('Prepend test nonce')}</FormLabel>
                <FormDescription>
                  {t(
                    'Add a unique nonce to each text test request to avoid prompt-cache reuse.'
                  )}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value === true}
                  disabled={props.sensitiveLocked}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />
      </div>

      <FormField
        control={props.form.control}
        name='failure_sample_replay_enabled'
        render={({ field }) => (
          <FormItem
            className='flex items-center justify-between'
            data-disabled={props.sensitiveLocked || undefined}
          >
            <div className='space-y-0.5'>
              <FormLabel>
                {t('Replay failure sample before recovery')}
              </FormLabel>
              <FormDescription>
                {t(
                  'Require a fresh replay of the last real failure before automatically re-enabling this channel.'
                )}
              </FormDescription>
            </div>
            <FormControl>
              <Switch
                checked={field.value === true}
                disabled={props.sensitiveLocked}
                onCheckedChange={field.onChange}
              />
            </FormControl>
          </FormItem>
        )}
      />

      <FormField
        control={props.form.control}
        name='test_disable_threshold_seconds'
        render={({ field }) => (
          <FormItem
            className='max-w-sm'
            data-disabled={props.sensitiveLocked || undefined}
          >
            <FormLabel>{t('Test disable threshold (seconds)')}</FormLabel>
            <FormControl>
              <Input
                type='number'
                min={0}
                step={0.1}
                inputMode='decimal'
                disabled={props.sensitiveLocked}
                value={field.value ?? ''}
                onChange={(event) =>
                  field.onChange(
                    event.target.value === ''
                      ? undefined
                      : event.target.valueAsNumber
                  )
                }
              />
            </FormControl>
            <FormDescription>
              {t(
                'Leave blank to inherit the global threshold. Enter 0 to disable the total-duration check for this channel.'
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  )
}
