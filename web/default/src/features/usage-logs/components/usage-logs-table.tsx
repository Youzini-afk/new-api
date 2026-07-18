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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import type {
  ColumnDef,
  OnChangeFn,
  PaginationState,
} from '@tanstack/react-table'
import { useReducedMotion } from 'motion/react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { useMediaQuery } from '@/hooks'
import { useIsAdmin } from '@/hooks/use-admin'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { cn } from '@/lib/utils'

import {
  DEFAULT_LOGS_DATA,
  LOG_TYPE_ALL_VALUE,
  LOG_TYPE_ENUM,
  USAGE_LOGS_AUTO_REFRESH_INTERVAL_MS,
} from '../constants'
import type { UsageLog } from '../data/schema'
import { useColumnsByCategory } from '../lib/columns'
import { getUsageLogLiveKey, mergeUsageLogLiveFeed } from '../lib/live-feed'
import { fetchLogsByCategory, getLogQueryEndTime } from '../lib/utils'
import type { LogCategory } from '../types'
import { CommonLogsFilterBar } from './common-logs-filter-bar'
import { LiveUsageLogRow } from './live-usage-log-row'
import { TaskLogsFilterBar } from './task-logs-filter-bar'
import { UsageLogsMobileList } from './usage-logs-mobile-card'
import { useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')
const LIVE_ROW_HIGHLIGHT_MS = 1_800

const logTypeRowTint: Record<number, string> = {
  [LOG_TYPE_ENUM.ERROR]: 'bg-rose-50/40 dark:bg-rose-950/20',
  [LOG_TYPE_ENUM.REFUND]: 'bg-blue-50/30 dark:bg-blue-950/15',
}

function getColumnVisibilityStorageKey(
  logCategory: LogCategory,
  isAdmin: boolean
): string {
  return `usage-logs:${logCategory}:${isAdmin ? 'admin' : 'user'}:column-visibility`
}

function deserializeLogTypeFilter(value: unknown): unknown[] {
  let values: unknown[] = []
  if (Array.isArray(value)) {
    values = value
  } else if (value) {
    values = [value]
  }
  return values.filter((item) => String(item) !== LOG_TYPE_ALL_VALUE)
}

interface UsageLogsTableProps {
  logCategory: LogCategory
}

export function UsageLogsTable({ logCategory }: UsageLogsTableProps) {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const searchParams = route.useSearch()
  const { autoRefreshEnabled, setAutoRefreshEnabled } = useUsageLogsContext()
  const previousPageIndexRef = useRef<number | null>(null)
  const liveTableBodyRef = useRef<HTMLDivElement>(null)
  const liveFeedContextRef = useRef('')
  const liveLogsRef = useRef<UsageLog[]>([])
  const liveHighlightTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null
  )
  const [liveLogs, setLiveLogs] = useState<UsageLog[]>([])
  const [enteringLogKeys, setEnteringLogKeys] = useState<Set<string>>(
    () => new Set()
  )
  const shouldReduceMotion = useReducedMotion()

  const {
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 20 : 100 },
    globalFilter: { enabled: false },
    columnFilters: [
      {
        columnId: 'created_at',
        searchKey: 'type',
        type: 'array' as const,
        deserialize: deserializeLogTypeFilter,
      },
      { columnId: 'model_name', searchKey: 'model', type: 'string' as const },
      { columnId: 'token_name', searchKey: 'token', type: 'string' as const },
      { columnId: 'group', searchKey: 'group', type: 'string' as const },
      ...(isAdmin
        ? [
            {
              columnId: 'channel',
              searchKey: 'channel',
              type: 'string' as const,
            },
            {
              columnId: 'username',
              searchKey: 'username',
              type: 'string' as const,
            },
          ]
        : []),
    ],
  })

  const isAutoRefreshActive =
    autoRefreshEnabled && logCategory === 'common' && pagination.pageIndex === 0
  const liveFeedContextKey = useMemo(
    () =>
      JSON.stringify([
        logCategory,
        isAdmin,
        pagination.pageSize,
        columnFilters,
        searchParams,
      ]),
    [columnFilters, isAdmin, logCategory, pagination.pageSize, searchParams]
  )

  useEffect(() => {
    if (autoRefreshEnabled && logCategory !== 'common') {
      setAutoRefreshEnabled(false)
    }
  }, [autoRefreshEnabled, logCategory, setAutoRefreshEnabled])

  useEffect(() => {
    const previousPageIndex = previousPageIndexRef.current
    previousPageIndexRef.current = pagination.pageIndex

    if (
      autoRefreshEnabled &&
      previousPageIndex !== null &&
      previousPageIndex !== pagination.pageIndex &&
      pagination.pageIndex !== 0
    ) {
      setAutoRefreshEnabled(false)
    }
  }, [autoRefreshEnabled, pagination.pageIndex, setAutoRefreshEnabled])

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'logs',
      logCategory,
      isAdmin,
      pagination.pageIndex + 1,
      pagination.pageSize,
      columnFilters,
      searchParams,
      isAutoRefreshActive,
      t,
    ],
    queryFn: async () => {
      const effectiveSearchParams = isAutoRefreshActive
        ? { ...searchParams, endTime: getLogQueryEndTime().getTime() }
        : searchParams
      const result = await fetchLogsByCategory({
        logCategory,
        isAdmin,
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        searchParams: effectiveSearchParams,
        columnFilters,
      })

      if (!result?.success) {
        toast.error(result?.message || t('Failed to load logs'), {
          id: 'usage-logs-load-error',
        })
        return DEFAULT_LOGS_DATA
      }

      return result.data || DEFAULT_LOGS_DATA
    },
    placeholderData: (previousData, previousQuery) => {
      if (previousQuery?.queryKey[1] === logCategory) {
        return previousData
      }
      return undefined
    },
    refetchInterval: isAutoRefreshActive
      ? USAGE_LOGS_AUTO_REFRESH_INTERVAL_MS
      : false,
    refetchIntervalInBackground: false,
  })

  const rawLogs = data?.items || DEFAULT_LOGS_DATA.items

  useEffect(() => {
    if (isAutoRefreshActive) return

    liveFeedContextRef.current = ''
    liveLogsRef.current = []
    setLiveLogs([])
    setEnteringLogKeys((current) =>
      current.size > 0 ? new Set<string>() : current
    )
    if (liveHighlightTimerRef.current) {
      clearTimeout(liveHighlightTimerRef.current)
      liveHighlightTimerRef.current = null
    }
  }, [isAutoRefreshActive])

  useEffect(() => {
    if (!isAutoRefreshActive) return

    const incomingLogs = rawLogs as UsageLog[]
    if (liveFeedContextRef.current !== liveFeedContextKey) {
      liveFeedContextRef.current = liveFeedContextKey
      liveLogsRef.current = incomingLogs
      setLiveLogs(incomingLogs)
      setEnteringLogKeys((current) =>
        current.size > 0 ? new Set<string>() : current
      )
      return
    }

    const merged = mergeUsageLogLiveFeed(
      liveLogsRef.current,
      incomingLogs,
      pagination.pageSize
    )
    if (merged.newKeys.length === 0) return

    liveLogsRef.current = merged.items
    setLiveLogs(merged.items)
    setEnteringLogKeys(new Set(merged.newKeys))

    if (liveHighlightTimerRef.current) {
      clearTimeout(liveHighlightTimerRef.current)
    }
    liveHighlightTimerRef.current = setTimeout(() => {
      setEnteringLogKeys(new Set())
      liveHighlightTimerRef.current = null
    }, LIVE_ROW_HIGHLIGHT_MS)
  }, [isAutoRefreshActive, liveFeedContextKey, pagination.pageSize, rawLogs])

  useEffect(
    () => () => {
      if (liveHighlightTimerRef.current) {
        clearTimeout(liveHighlightTimerRef.current)
      }
    },
    []
  )

  useEffect(() => {
    if (!isAutoRefreshActive || isMobile) return

    const frame = requestAnimationFrame(() => {
      liveTableBodyRef.current?.scrollTo({ top: 0, behavior: 'auto' })
    })
    return () => cancelAnimationFrame(frame)
  }, [isAutoRefreshActive, isMobile, liveFeedContextKey])

  useEffect(() => {
    if (!isAutoRefreshActive || isMobile || enteringLogKeys.size === 0) return

    const frame = requestAnimationFrame(() => {
      liveTableBodyRef.current?.scrollTo({
        top: 0,
        behavior: shouldReduceMotion ? 'auto' : 'smooth',
      })
    })
    return () => cancelAnimationFrame(frame)
  }, [enteringLogKeys, isAutoRefreshActive, isMobile, shouldReduceMotion])

  const logs =
    isAutoRefreshActive && liveFeedContextRef.current === liveFeedContextKey
      ? liveLogs
      : rawLogs
  const columns = useColumnsByCategory(logCategory, isAdmin)
  const isLoadingData = isLoading || (isFetching && !data)
  const handlePaginationChange: OnChangeFn<PaginationState> = (updater) => {
    if (autoRefreshEnabled) {
      setAutoRefreshEnabled(false)
    }
    onPaginationChange(updater)
  }
  const getRowId = useCallback(
    (row: Record<string, unknown>) => {
      if (logCategory === 'common') {
        return getUsageLogLiveKey(row as unknown as UsageLog)
      }
      return String(row.id)
    },
    [logCategory]
  )

  const { table } = useDataTable({
    data: logs as Record<string, unknown>[],
    columns: columns as ColumnDef<Record<string, unknown>>[],
    columnFilters,
    columnVisibilityStorageKey: getColumnVisibilityStorageKey(
      logCategory,
      isAdmin
    ),
    pagination,
    getRowId,
    enableRowSelection: false,
    onPaginationChange: handlePaginationChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  const isCommon = logCategory === 'common'
  const getLogColumnClassName = useCallback(
    () => (isCommon ? 'py-2' : 'py-3.5'),
    [isCommon]
  )

  return (
    <DataTablePage
      table={table}
      columns={columns as ColumnDef<Record<string, unknown>>[]}
      isLoading={isLoadingData}
      isFetching={isFetching && !isAutoRefreshActive}
      emptyTitle={t('No Logs Found')}
      emptyDescription={t(
        'No usage logs available. Logs will appear here once API calls are made.'
      )}
      skeletonKeyPrefix='usage-log-skeleton'
      applyHeaderSize
      tableBodyRef={liveTableBodyRef}
      tableClassName={cn(
        '[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
      )}
      mobile={
        <UsageLogsMobileList
          table={table}
          isLoading={isLoadingData}
          logCategory={logCategory}
          enteringRowIds={enteringLogKeys}
        />
      }
      toolbar={
        isCommon ? (
          <CommonLogsFilterBar table={table} />
        ) : (
          <TaskLogsFilterBar table={table} logCategory={logCategory} />
        )
      }
      renderRow={(row) => {
        const logType = (row.original as Record<string, unknown>).type as
          | number
          | undefined
        const tintClass =
          isCommon && logType != null ? (logTypeRowTint[logType] ?? '') : ''
        const isEntering = isAutoRefreshActive && enteringLogKeys.has(row.id)
        const rowClassName = cn(
          'transition-colors duration-700',
          tintClass,
          isEntering && 'bg-emerald-500/10 dark:bg-emerald-400/10'
        )

        if (isAutoRefreshActive && !shouldReduceMotion) {
          return (
            <LiveUsageLogRow
              key={row.id}
              row={row}
              className={rowClassName}
              getColumnClassName={getLogColumnClassName}
              cellRenderColumns={table.options.columns}
              isEntering={isEntering}
            />
          )
        }

        return (
          <DataTableRow
            key={row.id}
            row={row}
            className={rowClassName}
            getColumnClassName={getLogColumnClassName}
          />
        )
      }}
    />
  )
}
