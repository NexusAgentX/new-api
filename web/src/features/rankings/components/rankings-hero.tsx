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
import { useTranslation } from 'react-i18next'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import type { RankingPeriod, RankingView } from '../types'

const PERIODS: { id: RankingPeriod; labelKey: string }[] = [
  { id: 'today', labelKey: 'Today' },
  { id: 'week', labelKey: 'Week' },
  { id: 'month', labelKey: 'Month' },
  { id: 'year', labelKey: 'Year' },
]

type RankingsHeroProps = {
  view: RankingView
  period: RankingPeriod
  canViewUsers: boolean
  onViewChange: (view: RankingView) => void
  onPeriodChange: (period: RankingPeriod) => void
}

export function RankingsHero(props: RankingsHeroProps) {
  const { t } = useTranslation()
  const periods =
    props.view === 'users'
      ? PERIODS.filter((period) => period.id !== 'year')
      : PERIODS
  const description =
    props.view === 'users'
      ? t(
          'See recent user consumption and the models behind it, based on rolling usage data.'
        )
      : t(
          'Discover the most-used models and rising vendors on the platform, updated from live usage data.'
        )

  return (
    <section className='space-y-6'>
      <div className='space-y-2'>
        <h1 className='text-[clamp(1.75rem,4vw,2.5rem)] leading-[1.15] font-bold'>
          {t('Rankings')}
        </h1>
        <p className='text-muted-foreground/80 max-w-2xl text-sm'>
          {description}
        </p>
      </div>

      <div className='flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between'>
        <Tabs
          value={props.view}
          onValueChange={(value) => props.onViewChange(value as RankingView)}
        >
          <TabsList aria-label={t('Rankings')} className='w-full sm:w-auto'>
            <TabsTrigger value='models' className='min-w-24'>
              {t('Models')}
            </TabsTrigger>
            {props.canViewUsers && (
              <TabsTrigger value='users' className='min-w-24'>
                {t('Users')}
              </TabsTrigger>
            )}
          </TabsList>
        </Tabs>

        <div
          role='tablist'
          aria-label={t('Period')}
          className='border-border/60 flex min-w-0 items-center overflow-x-auto border-b'
        >
          {periods.map((period) => {
            const isActive = props.period === period.id
            return (
              <button
                key={period.id}
                role='tab'
                type='button'
                aria-selected={isActive}
                onClick={() => props.onPeriodChange(period.id)}
                className={cn(
                  'focus-visible:ring-ring/40 relative -mb-px shrink-0 rounded-sm px-3 py-2 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none',
                  isActive
                    ? 'text-foreground'
                    : 'text-muted-foreground hover:text-foreground'
                )}
              >
                {t(period.labelKey)}
                <span
                  aria-hidden
                  className={cn(
                    'bg-foreground absolute inset-x-3 -bottom-px h-0.5 transition-opacity',
                    isActive ? 'opacity-100' : 'opacity-0'
                  )}
                />
              </button>
            )
          })}
        </div>
      </div>
    </section>
  )
}
