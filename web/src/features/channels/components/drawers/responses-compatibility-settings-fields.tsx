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
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'

import type { ChannelFormValues } from '../../lib'

type ResponsesCompatibilitySettingsFieldsProps = {
  form: UseFormReturn<ChannelFormValues>
  currentType: number
  sensitiveLocked: boolean
}

export function ResponsesCompatibilitySettingsFields(
  props: ResponsesCompatibilitySettingsFieldsProps
) {
  const { t } = useTranslation()
  const compatibilityEnabled = props.form.watch('responses_compatibility_fix')

  if (props.currentType !== 1) return null

  return (
    <>
      <FormField
        control={props.form.control}
        name='force_format'
        render={({ field }) => (
          <FormItem
            className='flex items-center justify-between gap-3 px-4 py-3'
            data-disabled={props.sensitiveLocked || undefined}
          >
            <div className='space-y-0.5'>
              <FormLabel>{t('Force Format')}</FormLabel>
              <FormDescription>
                {t(
                  'Reformat Chat Completions responses (OpenAI channels only)'
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
        name='responses_compatibility_fix'
        render={({ field }) => (
          <FormItem
            className='flex items-center justify-between gap-3 px-4 py-3'
            data-disabled={props.sensitiveLocked || undefined}
          >
            <div className='space-y-0.5'>
              <FormLabel>{t('Responses compatibility fix')}</FormLabel>
              <FormDescription>
                {t(
                  'Normalize non-standard Item IDs and protect store:false history across channel replay'
                )}
              </FormDescription>
            </div>
            <FormControl>
              <Switch
                checked={field.value !== false}
                disabled={props.sensitiveLocked}
                onCheckedChange={field.onChange}
              />
            </FormControl>
          </FormItem>
        )}
      />

      <FormField
        control={props.form.control}
        name='allow_reasoning_without_encrypted_content'
        render={({ field }) => (
          <FormItem
            className='flex items-center justify-between gap-3 px-4 py-3'
            data-disabled={
              props.sensitiveLocked ||
              compatibilityEnabled === false ||
              undefined
            }
          >
            <div className='space-y-0.5'>
              <FormLabel>
                {t('Allow reasoning without encrypted content')}
              </FormLabel>
              <FormDescription>
                {t(
                  'Enable only after confirming the target upstream accepts summary-only reasoning replay'
                )}
              </FormDescription>
            </div>
            <FormControl>
              <Switch
                checked={field.value === true}
                disabled={
                  props.sensitiveLocked || compatibilityEnabled === false
                }
                onCheckedChange={field.onChange}
              />
            </FormControl>
          </FormItem>
        )}
      />
    </>
  )
}
