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
import { DatabaseZap, UsersRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import type { UserRankingsSnapshot, UserRankingSort } from '../types'
import { UserRankingTable } from './user-ranking-table'

const SORT_OPTIONS: { id: UserRankingSort; labelKey: string }[] = [
  { id: 'tokens', labelKey: 'Tokens' },
  { id: 'quota', labelKey: 'Gross quota' },
  { id: 'count', labelKey: 'Consumption count' },
]

type UserRankingsSectionProps = {
  snapshot: UserRankingsSnapshot
  sort: UserRankingSort
  onSortChange: (sort: UserRankingSort) => void
}

export function UserRankingsSection(props: UserRankingsSectionProps) {
  const { t } = useTranslation()
  const generatedDate = new Date(props.snapshot.generated_at)
  const generatedAt = Number.isNaN(generatedDate.getTime())
    ? props.snapshot.generated_at
    : generatedDate.toLocaleString()

  if (!props.snapshot.data_available) {
    return (
      <Alert className='py-4'>
        <DatabaseZap />
        <AlertTitle>{t('Usage statistics unavailable')}</AlertTitle>
        <AlertDescription>
          {t(
            'Data export is disabled, so user usage rankings are unavailable.'
          )}
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <section className='space-y-5'>
      <div className='flex flex-col gap-4 md:flex-row md:items-end md:justify-between'>
        <div className='space-y-1.5'>
          <h2 className='text-xl font-semibold'>{t('Users')}</h2>
          <p className='text-muted-foreground max-w-2xl text-sm'>
            {t(
              'Rolling quota statistics may be delayed by the data export interval and up to five minutes of caching.'
            )}
          </p>
        </div>
        <div className='flex flex-col items-start gap-2 sm:flex-row sm:items-center'>
          <span className='text-muted-foreground text-xs font-medium'>
            {t('Sort')}
          </span>
          <Tabs
            value={props.sort}
            onValueChange={(value) =>
              props.onSortChange(value as UserRankingSort)
            }
          >
            <TabsList aria-label={t('Sort')}>
              {SORT_OPTIONS.map((option) => (
                <TabsTrigger key={option.id} value={option.id}>
                  {t(option.labelKey)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </div>
      </div>

      {props.snapshot.users.length === 0 ? (
        <div className='border-border/70 flex flex-col items-center border-y py-16 text-center'>
          <UsersRound className='text-muted-foreground mb-3 size-6' />
          <h3 className='font-medium'>{t('No user usage in this period')}</h3>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Usage will appear after consumption statistics are exported.')}
          </p>
        </div>
      ) : (
        <UserRankingTable
          key={`${props.snapshot.period}:${props.sort}:${props.snapshot.generated_at}`}
          rows={props.snapshot.users}
          sort={props.sort}
        />
      )}

      <div className='text-muted-foreground flex flex-col gap-2 text-xs sm:flex-row sm:items-start sm:justify-between'>
        <p className='max-w-3xl'>
          {t(
            'Count reflects consumption records. Tokens include prompt and completion tokens. Gross quota does not subtract refunds or negative adjustments.'
          )}
        </p>
        <p className='shrink-0'>
          {t('Last updated:')} {generatedAt}
        </p>
      </div>
    </section>
  )
}
