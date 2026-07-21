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
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  convertDisplayAmountToUSD,
  convertUSDToDisplayAmount,
  getCurrencyLabel,
} from '@/lib/currency'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const MAX_INFRASTRUCTURE_DAILY_COST_USD = 1_000_000_000

const schema = z.object({
  infrastructureDailyCostUSD: z
    .number()
    .min(0)
    .max(MAX_INFRASTRUCTURE_DAILY_COST_USD),
})

type Values = z.infer<typeof schema>

export function OperatingCostsSection(props: {
  defaultValues: { infrastructureDailyCostUSD: number }
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyLabel === 'Tokens'
  const displayCurrencyLabel = tokensOnly ? t('Tokens') : currencyLabel
  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: props.defaultValues,
  })
  const { isDirty, isSubmitting } = form.formState

  async function onSubmit(values: Values) {
    if (
      values.infrastructureDailyCostUSD ===
      props.defaultValues.infrastructureDailyCostUSD
    ) {
      toast.info(t('No changes to save'))
      return
    }

    await updateOption.mutateAsync({
      key: 'finance_setting.infrastructure_daily_cost_usd',
      value: String(values.infrastructureDailyCostUSD),
    })
    form.reset(values)
  }

  return (
    <SettingsSection title={t('Operating Costs')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
          />
          <FormField
            control={form.control}
            name='infrastructureDailyCostUSD'
            render={({ field }) => (
              <FormItem className='max-w-sm'>
                <FormLabel>
                  {t('Daily Infrastructure Cost ({{currency}})', {
                    currency: displayCurrencyLabel,
                  })}
                </FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    max={convertUSDToDisplayAmount(
                      MAX_INFRASTRUCTURE_DAILY_COST_USD
                    )}
                    step={tokensOnly ? 1 : '0.000001'}
                    inputMode='decimal'
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                    value={convertUSDToDisplayAmount(field.value)}
                    onChange={(event) =>
                      field.onChange(
                        event.target.value === ''
                          ? 0
                          : convertDisplayAmountToUSD(
                              Number(event.target.value)
                            )
                      )
                    }
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Daily cost for servers and other infrastructure. Applied to every calendar day in finance reports.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
