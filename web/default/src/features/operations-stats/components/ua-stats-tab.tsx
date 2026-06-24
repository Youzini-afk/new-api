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
 * User Agents tab for operations stats page.
 */
import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Users } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { formatCompactNumber } from '@/lib/format'
import { CopyButton } from '@/components/copy-button'
import { getUAStatsRank, getUAStatsUsers } from '../api'
import {
  DEFAULT_PAGE_SIZE,
  DEFAULT_RANK_LIMIT,
  DEFAULT_UA_MATCH_MODE,
  RANK_LIMIT_OPTIONS,
} from '../constants'
import type { UAStatsRankItem, UAStatsUserItem } from '../types'
import { TimeRangeFilter, type TimeRangeFilterValue } from './time-range-filter'
import { RankTable } from './rank-table'
import { UsersPanel } from './users-panel'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

const DEFAULT_TIME_RANGE: TimeRangeFilterValue = { range_days: 7 }
const MIN_KEYWORD_LENGTH = 2

export function UAStatsTab() {
  const { t } = useTranslation()
  const [timeRange, setTimeRange] = useState<TimeRangeFilterValue>(
    DEFAULT_TIME_RANGE
  )
  const [limit, setLimit] = useState(DEFAULT_RANK_LIMIT)
  const [keyword, setKeyword] = useState('')
  const [appliedKeyword, setAppliedKeyword] = useState('')
  const [selectedUA, setSelectedUA] = useState<string | null>(null)
  const [matchMode] = useState(DEFAULT_UA_MATCH_MODE)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [usersPage, setUsersPage] = useState(1)

  const rankParams = useMemo(
    () => ({
      limit,
      keyword: appliedKeyword,
      range_days: timeRange.range_days,
      start_timestamp: timeRange.start_timestamp,
      end_timestamp: timeRange.end_timestamp,
    }),
    [limit, appliedKeyword, timeRange]
  )

  const {
    data: rankResponse,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: ['operations-stats', 'ua-rank', rankParams],
    queryFn: async () => {
      const result = await getUAStatsRank(rankParams)
      if (!result.success) {
        throw new Error(result.message || t('Failed to load user agent stats'))
      }
      return result.data
    },
  })

  const usersParams = useMemo(
    () =>
      selectedUA
        ? {
            ua: selectedUA,
            match: matchMode,
            range_days: timeRange.range_days,
            start_timestamp: timeRange.start_timestamp,
            end_timestamp: timeRange.end_timestamp,
            p: usersPage,
            page_size: DEFAULT_PAGE_SIZE,
          }
        : null,
    [selectedUA, matchMode, timeRange, usersPage]
  )

  const { data: usersResponse, isLoading: usersLoading } = useQuery({
    queryKey: ['operations-stats', 'ua-users', usersParams],
    queryFn: async () => {
      if (!usersParams) return null
      const result = await getUAStatsUsers(usersParams)
      if (!result.success) {
        throw new Error(result.message || t('Failed to load users'))
      }
      return result.data
    },
    enabled: Boolean(usersParams),
  })

  const applyKeyword = () => {
    const trimmed = keyword.trim()
    if (trimmed && trimmed.length < MIN_KEYWORD_LENGTH) {
      toast.error(
        t('Keyword must be at least {{count}} characters', {
          count: MIN_KEYWORD_LENGTH,
        })
      )
      return
    }
    setAppliedKeyword(trimmed)
  }

  const handleViewUsers = (ua: string) => {
    setSelectedUA(ua)
    setUsersPage(1)
    setDrawerOpen(true)
  }

  const items = rankResponse?.items ?? []

  return (
    <div className='flex h-full min-h-0 flex-col gap-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
        <div className='flex flex-wrap items-center gap-2'>
          <TimeRangeFilter value={timeRange} onChange={setTimeRange} />

          <div className='flex items-center gap-2'>
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') applyKeyword()
              }}
              placeholder={t('Filter by keyword')}
              className='w-[200px]'
            />
            <Button variant='secondary' size='sm' onClick={applyKeyword}>
              {t('Filter')}
            </Button>
          </div>
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

      {appliedKeyword && (
        <div className='text-muted-foreground text-xs'>
          {t('Filtering by: {{keyword}}', { keyword: appliedKeyword })}
        </div>
      )}

      <RankTable<UAStatsRankItem>
        items={items}
        isLoading={isLoading}
        error={error}
        onRetry={refetch}
        emptyTitle={t('No user agents found')}
        emptyDescription={t(
          'No API calls with identifiable user agents in the selected time range.'
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
            key: 'user_agent',
            title: t('User Agent'),
            render: (item) => (
              <div className='flex items-center gap-2'>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <span className='max-w-[300px] truncate font-mono text-xs sm:max-w-[400px]'>
                        {item.user_agent}
                      </span>
                    }
                  />
                  <TooltipContent className='max-w-sm break-all'>
                    {item.user_agent}
                  </TooltipContent>
                </Tooltip>
                <CopyButton
                  value={item.user_agent}
                  size='icon'
                  className='size-6'
                  variant='ghost'
                  tooltip={t('Copy User Agent')}
                />
              </div>
            ),
          },
          {
            key: 'count',
            title: t('Count'),
            align: 'right',
            className: 'w-28',
            render: (item) => formatCompactNumber(item.count),
          },
          {
            key: 'actions',
            title: '',
            align: 'right',
            className: 'w-28',
            render: (item) => (
              <Button
                variant='ghost'
                size='sm'
                className='gap-1'
                onClick={() => handleViewUsers(item.user_agent)}
              >
                <Users className='size-3.5' />
                {t('Users')}
              </Button>
            ),
          },
        ]}
        className='flex-1'
      />

      {rankResponse && (
        <p className='text-muted-foreground text-xs'>
          {t('Total User Agents: {{total}}', {
            total: formatCompactNumber(rankResponse.total_uas),
          })}
        </p>
      )}

      <UsersPanel
        open={drawerOpen}
        onOpenChange={(open) => {
          setDrawerOpen(open)
          if (!open) setSelectedUA(null)
        }}
        title={
          selectedUA
            ? t('Users with User Agent')
            : t('Users with User Agent')
        }
        description={selectedUA ? selectedUA : ''}
        items={(usersResponse?.items ?? []) as UAStatsUserItem[]}
        total={usersResponse?.total ?? 0}
        page={usersPage}
        pageSize={DEFAULT_PAGE_SIZE}
        onPageChange={setUsersPage}
        isLoading={usersLoading}
        emptyTitle={t('No users found')}
        emptyDescription={t(
          'No users used this user agent in the selected time range.'
        )}
      />
    </div>
  )
}
