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
import { useNavigate, useSearch } from '@tanstack/react-router'
import { lazy, Suspense, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuthStore } from '@/stores/auth-store'

import { RankingsHero } from './components/rankings-hero'
import { UserRankingsSection } from './components/user-rankings-section'
import { useRankings } from './hooks/use-rankings'
import { useUserRankings } from './hooks/use-user-rankings'
import type {
  RankingPeriod,
  RankingView,
  RankingsSnapshot,
  UserRankingPeriod,
  UserRankingSort,
} from './types'

const ModelsSection = lazy(() =>
  import('./components/models-section').then((module) => ({
    default: module.ModelsSection,
  }))
)
const MarketShareSection = lazy(() =>
  import('./components/market-share-section').then((module) => ({
    default: module.MarketShareSection,
  }))
)
const PulseSection = lazy(() =>
  import('./components/pulse-section').then((module) => ({
    default: module.PulseSection,
  }))
)

export function Rankings() {
  const { t } = useTranslation()
  const search = useSearch({ from: '/rankings/' })
  const navigate = useNavigate()
  const user = useAuthStore((state) => state.auth.user)
  const canViewUsers = Boolean(user)
  const view: RankingView =
    canViewUsers && search.view === 'users' ? 'users' : 'models'
  const requestedPeriod: RankingPeriod =
    search.period ?? (view === 'users' ? 'today' : 'week')
  let period: RankingPeriod = requestedPeriod
  if (view === 'users' && requestedPeriod === 'year') {
    period = 'today'
  }
  const userPeriod = period as UserRankingPeriod
  const sort: UserRankingSort = search.sort ?? 'tokens'

  useEffect(() => {
    if (view !== 'users' || search.period !== 'year') return
    navigate({
      to: '/rankings',
      replace: true,
      search: { ...search, period: 'today' },
    })
  }, [navigate, search, view])

  const rankingsQuery = useRankings(period, view === 'models')
  const userRankingsQuery = useUserRankings(
    userPeriod,
    sort,
    view === 'users' && canViewUsers
  )

  const handleViewChange = (nextView: RankingView) => {
    let nextPeriod = search.period
    if (nextView === 'users' && (!search.period || search.period === 'year')) {
      nextPeriod = 'today'
    }
    navigate({
      to: '/rankings',
      search: { ...search, view: nextView, period: nextPeriod },
    })
  }

  const handlePeriodChange = (nextPeriod: RankingPeriod) => {
    navigate({
      to: '/rankings',
      search: { ...search, period: nextPeriod },
    })
  }

  const handleSortChange = (nextSort: UserRankingSort) => {
    navigate({
      to: '/rankings',
      search: { ...search, sort: nextSort },
    })
  }

  const activeQuery = view === 'models' ? rankingsQuery : userRankingsQuery
  const errorMessage =
    activeQuery.error instanceof Error
      ? activeQuery.error.message
      : t('Unable to load rankings data')

  let content
  if (activeQuery.isLoading) {
    content = <RankingsLoading />
  } else if (view === 'models') {
    const snapshot = rankingsQuery.data?.data
    content = snapshot ? (
      <Suspense fallback={<RankingsLoading />}>
        <ModelRankingsContent snapshot={snapshot} period={period} />
      </Suspense>
    ) : (
      <RankingsError message={errorMessage} />
    )
  } else {
    const snapshot = userRankingsQuery.data?.data
    content = snapshot ? (
      <UserRankingsSection
        snapshot={snapshot}
        sort={sort}
        onSortChange={handleSortChange}
      />
    ) : (
      <RankingsError message={errorMessage} />
    )
  }

  return (
    <PublicLayout showMainContainer={false}>
      <PageTransition className='mx-auto w-full max-w-[1280px] space-y-8 px-3 pt-16 pb-10 sm:px-6 sm:pt-20 sm:pb-12 xl:px-8'>
        <RankingsHero
          view={view}
          period={period}
          canViewUsers={canViewUsers}
          onViewChange={handleViewChange}
          onPeriodChange={handlePeriodChange}
        />

        {content}
      </PageTransition>
    </PublicLayout>
  )
}

function ModelRankingsContent(props: {
  snapshot: RankingsSnapshot
  period: RankingPeriod
}) {
  return (
    <div className='space-y-8'>
      <ModelsSection
        history={props.snapshot.models_history}
        rows={props.snapshot.models}
        period={props.period}
      />
      <MarketShareSection
        history={props.snapshot.vendor_share_history}
        rows={props.snapshot.vendors}
        period={props.period}
      />
      <PulseSection
        movers={props.snapshot.top_movers}
        droppers={props.snapshot.top_droppers}
      />
    </div>
  )
}

function RankingsLoading() {
  return (
    <div className='space-y-6'>
      <Skeleton className='h-[420px] w-full rounded-lg' />
      <Skeleton className='h-[240px] w-full rounded-lg' />
    </div>
  )
}

function RankingsError(props: { message: string }) {
  const { t } = useTranslation()
  return (
    <div className='border-border/70 border-y px-6 py-12 text-center'>
      <h2 className='text-foreground text-base font-semibold'>
        {t('Unable to load rankings')}
      </h2>
      <p className='text-muted-foreground mx-auto mt-2 max-w-md text-sm'>
        {props.message}
      </p>
    </div>
  )
}
