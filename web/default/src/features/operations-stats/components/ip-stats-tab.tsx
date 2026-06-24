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
 * IP Stats tab for operations stats page.
 */
import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Users } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { formatCompactNumber } from '@/lib/format'
import { CopyButton } from '@/components/copy-button'
import { getIPStatsRank, getIPStatsUsers } from '../api'
import {
  CONVERSATION_KINDS,
  DEFAULT_CONVERSATION_KIND,
  DEFAULT_PAGE_SIZE,
  DEFAULT_RANK_LIMIT,
  RANK_LIMIT_OPTIONS,
} from '../constants'
import type { IPStatsRankItem, IPStatsUserItem } from '../types'
import { TimeRangeFilter, type TimeRangeFilterValue } from './time-range-filter'
import { RankTable } from './rank-table'
import { UsersPanel } from './users-panel'

const DEFAULT_TIME_RANGE: TimeRangeFilterValue = { range_days: 1 }

export function IPStatsTab() {
  const { t } = useTranslation()
  const [kind, setKind] = useState(DEFAULT_CONVERSATION_KIND)
  const [timeRange, setTimeRange] = useState<TimeRangeFilterValue>(
    DEFAULT_TIME_RANGE
  )
  const [limit, setLimit] = useState(DEFAULT_RANK_LIMIT)
  const [selectedIP, setSelectedIP] = useState<string | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [usersPage, setUsersPage] = useState(1)

  const rankParams = useMemo(
    () => ({
      kind,
      limit,
      range_days: timeRange.range_days,
      start_timestamp: timeRange.start_timestamp,
      end_timestamp: timeRange.end_timestamp,
    }),
    [kind, limit, timeRange]
  )

  const {
    data: rankResponse,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: ['operations-stats', 'ip-rank', rankParams],
    queryFn: async () => {
      const result = await getIPStatsRank(rankParams)
      if (!result.success) {
        throw new Error(result.message || t('Failed to load IP stats'))
      }
      return result.data
    },
  })

  const usersParams = useMemo(
    () =>
      selectedIP
        ? {
            ip: selectedIP,
            kind,
            range_days: timeRange.range_days,
            start_timestamp: timeRange.start_timestamp,
            end_timestamp: timeRange.end_timestamp,
            p: usersPage,
            page_size: DEFAULT_PAGE_SIZE,
          }
        : null,
    [selectedIP, kind, timeRange, usersPage]
  )

  const { data: usersResponse, isLoading: usersLoading } = useQuery({
    queryKey: ['operations-stats', 'ip-users', usersParams],
    queryFn: async () => {
      if (!usersParams) return null
      const result = await getIPStatsUsers(usersParams)
      if (!result.success) {
        throw new Error(result.message || t('Failed to load users'))
      }
      return result.data
    },
    enabled: Boolean(usersParams),
  })

  const handleViewUsers = (ip: string) => {
    setSelectedIP(ip)
    setUsersPage(1)
    setDrawerOpen(true)
  }

  const items = rankResponse?.items ?? []

  return (
    <div className='flex h-full min-h-0 flex-col gap-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <div className='flex flex-wrap items-center gap-2'>
          <Select value={kind} onValueChange={(v) => setKind(v as typeof kind)}>
            <SelectTrigger className='w-[180px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {CONVERSATION_KINDS.map((k) => (
                <SelectItem key={k.value} value={k.value}>
                  {t(k.labelKey)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <TimeRangeFilter value={timeRange} onChange={setTimeRange} />
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

      <RankTable<IPStatsRankItem>
        items={items}
        isLoading={isLoading}
        error={error}
        onRetry={refetch}
        emptyTitle={t('No IP stats found')}
        emptyDescription={t(
          'No API calls from unique IPs in the selected time range.'
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
            key: 'ip',
            title: t('IP'),
            render: (item) => (
              <div className='flex items-center gap-2'>
                <span className='font-mono text-xs'>{item.ip}</span>
                <CopyButton
                  value={item.ip}
                  size='icon'
                  variant='ghost'
                  tooltip={t('Copy IP')}
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
                onClick={() => handleViewUsers(item.ip)}
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
          {t('Total IPs: {{total}}', {
            total: formatCompactNumber(rankResponse.total_ips),
          })}
        </p>
      )}

      <UsersPanel
        open={drawerOpen}
        onOpenChange={(open) => {
          setDrawerOpen(open)
          if (!open) setSelectedIP(null)
        }}
        title={
          selectedIP
            ? t('Users behind {{ip}}', { ip: selectedIP })
            : t('Users behind IP')
        }
        description={t(
          'Users that made API calls from this IP during the selected time range.'
        )}
        items={(usersResponse?.items ?? []) as IPStatsUserItem[]}
        total={usersResponse?.total ?? 0}
        page={usersPage}
        pageSize={DEFAULT_PAGE_SIZE}
        onPageChange={setUsersPage}
        isLoading={usersLoading}
        emptyTitle={t('No users found')}
        emptyDescription={t(
          'No users made API calls from this IP in the selected time range.'
        )}
      />
    </div>
  )
}
