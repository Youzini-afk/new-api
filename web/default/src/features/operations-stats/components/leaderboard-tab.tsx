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
/**
 * User Leaderboard tab for operations stats page.
 */
import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { formatCompactNumber, formatQuota } from '@/lib/format'
import {
  getUserLeaderboardCoverage,
  getUserLeaderboardRank,
} from '../api'
import {
  COVERAGE_SLOT_OPTIONS,
  DEFAULT_COVERAGE_SLOT_MINUTES,
  DEFAULT_LEADERBOARD_METRIC,
  DEFAULT_RANK_LIMIT,
  LEADERBOARD_METRICS,
  RANK_LIMIT_OPTIONS,
} from '../constants'
import type {
  LeaderboardCoverageItem,
  LeaderboardMetric,
  LeaderboardRankItem,
} from '../types'
import { TimeRangeFilter, type TimeRangeFilterValue } from './time-range-filter'
import { RankTable } from './rank-table'
import { formatTimestamp } from '../lib/utils'

const DEFAULT_TIME_RANGE: TimeRangeFilterValue = { range_days: 7 }

type LeaderboardMetricOrCoverage = LeaderboardMetric | 'coverage'

export function LeaderboardTab() {
  const { t } = useTranslation()
  const [metric, setMetric] = useState<LeaderboardMetricOrCoverage>(
    DEFAULT_LEADERBOARD_METRIC
  )
  const [timeRange, setTimeRange] = useState<TimeRangeFilterValue>(
    DEFAULT_TIME_RANGE
  )
  const [limit, setLimit] = useState(DEFAULT_RANK_LIMIT)
  const [slotMinutes, setSlotMinutes] = useState(
    DEFAULT_COVERAGE_SLOT_MINUTES
  )

  const isCoverage = metric === 'coverage'

  const rankParams = useMemo(
    () => ({
      metric,
      limit,
      range_days: timeRange.range_days,
      start_timestamp: timeRange.start_timestamp,
      end_timestamp: timeRange.end_timestamp,
    }),
    [metric, limit, timeRange]
  )

  const {
    data: rankResponse,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: ['operations-stats', 'leaderboard', rankParams],
    queryFn: async () => {
      if (isCoverage) {
        const result = await getUserLeaderboardCoverage({
          slot_minutes: slotMinutes,
          limit,
          range_days: timeRange.range_days,
          start_timestamp: timeRange.start_timestamp,
          end_timestamp: timeRange.end_timestamp,
        })
        if (!result.success) {
          throw new Error(
            result.message || t('Failed to load coverage leaderboard')
          )
        }
        return { coverage: result.data, rank: null }
      }
      const result = await getUserLeaderboardRank(rankParams)
      if (!result.success) {
        throw new Error(
          result.message || t('Failed to load user leaderboard')
        )
      }
      return { rank: result.data, coverage: null }
    },
  })

  const renderUser = (item: {
    username: string
    display_name: string
    remark: string
    user_id: number
  }) => (
    <div className='flex flex-col'>
      <span className='font-medium'>
        {item.username || `ID: ${item.user_id}`}
      </span>
      {item.display_name && (
        <span className='text-muted-foreground text-xs'>
          {item.display_name}
        </span>
      )}
      {item.remark && (
        <span className='text-muted-foreground text-xs'>{item.remark}</span>
      )}
    </div>
  )

  const rankItems = rankResponse?.rank?.items ?? []
  const coverageItems = rankResponse?.coverage?.items ?? []
  const selectedMetricLabel =
    LEADERBOARD_METRICS.find((m) => m.value === metric)?.labelKey ?? metric

  return (
    <div className='flex h-full min-h-0 flex-col gap-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <div className='flex flex-wrap items-center gap-2'>
          <TimeRangeFilter value={timeRange} onChange={setTimeRange} />

          <Select
            value={metric}
            onValueChange={(v) =>
              setMetric(v as LeaderboardMetricOrCoverage)
            }
          >
            <SelectTrigger className='w-[140px]'>
              <SelectValue>{t(selectedMetricLabel)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {LEADERBOARD_METRICS.map((m) => (
                <SelectItem key={m.value} value={m.value}>
                  {t(m.labelKey)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          {isCoverage && (
            <Select
              value={String(slotMinutes)}
              onValueChange={(v) => setSlotMinutes(Number(v))}
            >
              <SelectTrigger className='w-[120px]'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {COVERAGE_SLOT_OPTIONS.map((option) => (
                  <SelectItem key={option} value={String(option)}>
                    {option} {t('min')}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </div>

        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground text-xs'>{t('Top')}</span>
          <Select
            value={String(limit)}
            onValueChange={(v) => setLimit(Number(v))}
          >
            <SelectTrigger className='w-[100px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {RANK_LIMIT_OPTIONS.map((option) => (
                <SelectItem key={option} value={String(option)}>
                  {option}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {isCoverage ? (
        <RankTable<LeaderboardCoverageItem>
          items={coverageItems}
          isLoading={isLoading}
          error={error}
          onRetry={refetch}
          emptyTitle={t('No coverage data found')}
          emptyDescription={t(
            'No user activity coverage in the selected time range.'
          )}
          columns={[
            {
              key: 'rank',
              title: t('Rank'),
              className: 'w-16',
              align: 'center',
              render: (_, index) => index + 1,
            },
            {
              key: 'user',
              title: t('User'),
              render: (item) => renderUser(item),
            },
            {
              key: 'active_slots',
              title: t('Active Slots'),
              align: 'right',
              className: 'w-32',
              render: (item) => formatCompactNumber(item.active_slots),
            },
            {
              key: 'total_slots',
              title: t('Total Slots'),
              align: 'right',
              className: 'w-32',
              render: (item) => formatCompactNumber(item.total_slots),
            },
            {
              key: 'coverage',
              title: t('Coverage %'),
              align: 'right',
              className: 'w-32',
              render: (item) => `${item.coverage_pct.toFixed(2)}%`,
            },
          ]}
          className='flex-1'
        />
      ) : (
        <RankTable<LeaderboardRankItem>
          items={rankItems}
          isLoading={isLoading}
          error={error}
          onRetry={refetch}
          emptyTitle={t('No leaderboard data found')}
          emptyDescription={t(
            'No user activity in the selected time range.'
          )}
          columns={[
            {
              key: 'rank',
              title: t('Rank'),
              className: 'w-16',
              align: 'center',
              render: (_, index) => index + 1,
            },
            {
              key: 'user',
              title: t('User'),
              render: (item) => renderUser(item),
            },
            {
              key: 'call_count',
              title: t('Call Count'),
              align: 'right',
              className: 'w-28',
              render: (item) => formatCompactNumber(item.call_count),
            },
            {
              key: 'quota_sum',
              title: t('Quota Sum'),
              align: 'right',
              className: 'w-32',
              render: (item) => formatQuota(item.quota_sum),
            },
            {
              key: 'rph',
              title: t('RPH'),
              align: 'right',
              className: 'w-24',
              render: (item) => item.rph.toFixed(2),
            },
            {
              key: 'first_call',
              title: t('First Call'),
              align: 'right',
              className: 'w-36',
              render: (item) =>
                item.first_call
                  ? formatTimestamp(item.first_call, {
                      year: undefined,
                      month: '2-digit',
                      day: '2-digit',
                      hour: '2-digit',
                      minute: '2-digit',
                    })
                  : '-',
            },
            {
              key: 'last_call',
              title: t('Last Call'),
              align: 'right',
              className: 'w-36',
              render: (item) =>
                item.last_call
                  ? formatTimestamp(item.last_call, {
                      year: undefined,
                      month: '2-digit',
                      day: '2-digit',
                      hour: '2-digit',
                      minute: '2-digit',
                    })
                  : '-',
            },
          ]}
          className='flex-1'
        />
      )}
    </div>
  )
}
