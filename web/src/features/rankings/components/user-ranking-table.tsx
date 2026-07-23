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
import { ChevronDown } from 'lucide-react'
import { Fragment, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatNumber, formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import { formatShare, formatTokens } from '../lib/format'
import type { UserRanking, UserRankingModel, UserRankingSort } from '../types'

const SORT_LABEL_KEYS: Record<UserRankingSort, string> = {
  tokens: 'Tokens',
  quota: 'Gross quota',
  count: 'Consumption count',
}

type UserRankingTableProps = {
  rows: UserRanking[]
  sort: UserRankingSort
}

export function UserRankingTable(props: UserRankingTableProps) {
  const { t } = useTranslation()
  const [expandedRanks, setExpandedRanks] = useState<Set<number>>(
    () => new Set()
  )

  const toggleRank = (rank: number) => {
    setExpandedRanks((current) => {
      const next = new Set(current)
      if (next.has(rank)) {
        next.delete(rank)
      } else {
        next.add(rank)
      }
      return next
    })
  }

  return (
    <div className='border-border/70 overflow-hidden rounded-lg border'>
      <Table className='min-w-[820px]'>
        <TableHeader className='bg-muted/30'>
          <TableRow>
            <TableHead className='w-12 px-3' />
            <TableHead className='w-16'>{t('Rank')}</TableHead>
            <TableHead className='min-w-56'>{t('User')}</TableHead>
            <TableHead className='w-36 text-right'>
              {t('Consumption count')}
            </TableHead>
            <TableHead className='w-36 text-right'>{t('Tokens')}</TableHead>
            <TableHead className='w-40 pr-4 text-right'>
              {t('Gross quota')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.rows.map((row) => {
            const expanded = expandedRanks.has(row.rank)
            let displayName = row.display_name
            if (row.deleted) {
              displayName = t('Deleted user')
            } else if (row.display_name === 'User') {
              displayName = t('User')
            }
            const detailID = `user-ranking-models-${row.rank}`

            return (
              <Fragment key={row.rank}>
                <TableRow>
                  <TableCell className='px-3'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-sm'
                      aria-expanded={expanded}
                      aria-controls={detailID}
                      aria-label={t('Show top models for {{name}}', {
                        name: displayName,
                      })}
                      onClick={() => toggleRank(row.rank)}
                    >
                      <ChevronDown
                        className={cn(
                          'transition-transform',
                          expanded && 'rotate-180'
                        )}
                      />
                    </Button>
                  </TableCell>
                  <TableCell className='text-muted-foreground font-mono text-xs'>
                    {row.rank}
                  </TableCell>
                  <TableCell>
                    <div className='min-w-0'>
                      <div className='text-foreground max-w-52 truncate font-medium'>
                        {displayName}
                      </div>
                      {row.username && (
                        <div className='text-muted-foreground max-w-52 truncate font-mono text-xs'>
                          @{row.username}
                        </div>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className='text-right font-mono'>
                    {formatNumber(row.count)}
                  </TableCell>
                  <TableCell className='text-right font-mono'>
                    {formatTokens(row.tokens)}
                  </TableCell>
                  <TableCell className='pr-4 text-right font-mono'>
                    {formatQuota(row.quota)}
                  </TableCell>
                </TableRow>
                {expanded && (
                  <TableRow className='hover:bg-transparent'>
                    <TableCell
                      colSpan={6}
                      className='bg-muted/20 p-0 whitespace-normal'
                    >
                      <div id={detailID} className='px-6 py-5 pl-20'>
                        <div className='mb-3 flex items-center justify-between gap-4'>
                          <h3 className='text-sm font-medium'>
                            {t('Top models')}
                          </h3>
                          <span className='text-muted-foreground text-xs'>
                            {t(SORT_LABEL_KEYS[props.sort])}
                          </span>
                        </div>
                        {row.top_models.length === 0 ? (
                          <p className='text-muted-foreground py-3 text-sm'>
                            {t('No model usage details in this period.')}
                          </p>
                        ) : (
                          <ModelBreakdown rows={row.top_models} />
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

function ModelBreakdown(props: { rows: UserRankingModel[] }) {
  const { t } = useTranslation()
  return (
    <div className='space-y-1'>
      <div className='text-muted-foreground grid grid-cols-[minmax(180px,1fr)_100px_120px_130px_100px] gap-4 px-2 pb-1 text-xs'>
        <span>{t('Models')}</span>
        <span className='text-right'>{t('Consumption count')}</span>
        <span className='text-right'>{t('Tokens')}</span>
        <span className='text-right'>{t('Gross quota')}</span>
        <span className='text-right'>{t('Share')}</span>
      </div>
      {props.rows.map((row) => (
        <div
          key={row.model_name}
          className='grid grid-cols-[minmax(180px,1fr)_100px_120px_130px_100px] items-center gap-4 rounded-md px-2 py-2 text-sm'
        >
          <span className='truncate font-mono text-xs font-medium'>
            {row.model_name}
          </span>
          <span className='text-right font-mono text-xs'>
            {formatNumber(row.count)}
          </span>
          <span className='text-right font-mono text-xs'>
            {formatTokens(row.tokens)}
          </span>
          <span className='text-right font-mono text-xs'>
            {formatQuota(row.quota)}
          </span>
          <div className='flex items-center gap-2'>
            <Progress
              value={Math.min(100, Math.max(0, row.share * 100))}
              aria-label={`${row.model_name} ${t('Share')}`}
              className='min-w-0 flex-1 gap-0'
            />
            <span className='text-muted-foreground w-10 text-right font-mono text-[11px]'>
              {formatShare(row.share)}
            </span>
          </div>
        </div>
      ))}
    </div>
  )
}
