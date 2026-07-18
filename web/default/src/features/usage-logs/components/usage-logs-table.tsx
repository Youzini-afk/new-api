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
import {
  getLiveFeedInsertInterval,
  getLiveFeedInsertionOrder,
  getUsageLogLiveKey,
  mergeUsageLogLiveFeed,
} from '../lib/live-feed'
import { fetchLogsByCategory, getLogQueryEndTime } from '../lib/utils'
import type { LogCategory } from '../types'
import { CommonLogsFilterBar } from './common-logs-filter-bar'
import { LiveUsageLogRow } from './live-usage-log-row'
import { TaskLogsFilterBar } from './task-logs-filter-bar'
import { UsageLogsMobileList } from './usage-logs-mobile-card'
import { useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')
const LIVE_ROW_ENTER_DURATION_MS = 650
const LIVE_LAYOUT_ROW_LIMIT = 24

interface LiveFeedRenderState {
  logs: UsageLog[]
  enteringLogKeys: Set<string>
}

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
  const pendingLiveLogsRef = useRef<UsageLog[]>([])
  const liveInsertTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const liveInsertIntervalRef = useRef(0)
  const liveEntryTimersRef = useRef(
    new Map<string, ReturnType<typeof setTimeout>>()
  )
  const livePageSizeRef = useRef(100)
  const liveActiveRef = useRef(false)
  const [liveFeedState, setLiveFeedState] = useState<LiveFeedRenderState>(
    () => ({
      logs: [],
      enteringLogKeys: new Set(),
    })
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
  liveActiveRef.current = isAutoRefreshActive
  livePageSizeRef.current = pagination.pageSize

  const clearLiveFeedTimers = useCallback(() => {
    if (liveInsertTimerRef.current) {
      clearTimeout(liveInsertTimerRef.current)
      liveInsertTimerRef.current = null
    }
    for (const timer of liveEntryTimersRef.current.values()) {
      clearTimeout(timer)
    }
    liveEntryTimersRef.current.clear()
    pendingLiveLogsRef.current = []
    liveInsertIntervalRef.current = 0
  }, [])

  const commitLiveRowInsertion = useCallback(
    (logs: UsageLog[], key: string) => {
      setLiveFeedState((current) => {
        const enteringLogKeys = new Set(current.enteringLogKeys)
        enteringLogKeys.add(key)
        return { logs, enteringLogKeys }
      })

      const existingTimer = liveEntryTimersRef.current.get(key)
      if (existingTimer) clearTimeout(existingTimer)

      const timer = setTimeout(() => {
        setLiveFeedState((current) => {
          if (!current.enteringLogKeys.has(key)) return current
          const enteringLogKeys = new Set(current.enteringLogKeys)
          enteringLogKeys.delete(key)
          return { ...current, enteringLogKeys }
        })
        liveEntryTimersRef.current.delete(key)
      }, LIVE_ROW_ENTER_DURATION_MS)
      liveEntryTimersRef.current.set(key, timer)
    },
    []
  )

  const drainLiveFeedQueue = useCallback(() => {
    liveInsertTimerRef.current = null
    if (!liveActiveRef.current) {
      pendingLiveLogsRef.current = []
      return
    }

    const nextLog = pendingLiveLogsRef.current.shift()
    if (!nextLog) return

    const merged = mergeUsageLogLiveFeed(
      liveLogsRef.current,
      [nextLog],
      livePageSizeRef.current
    )
    if (merged.newKeys.length > 0) {
      liveLogsRef.current = merged.items
      commitLiveRowInsertion(merged.items, merged.newKeys[0])
    }

    if (pendingLiveLogsRef.current.length > 0) {
      liveInsertTimerRef.current = setTimeout(
        drainLiveFeedQueue,
        liveInsertIntervalRef.current
      )
    }
  }, [commitLiveRowInsertion])

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

    clearLiveFeedTimers()
    liveFeedContextRef.current = ''
    liveLogsRef.current = []
    setLiveFeedState({ logs: [], enteringLogKeys: new Set() })
  }, [clearLiveFeedTimers, isAutoRefreshActive])

  useEffect(() => {
    if (!isAutoRefreshActive) return

    const incomingLogs = rawLogs as UsageLog[]
    if (liveFeedContextRef.current !== liveFeedContextKey) {
      clearLiveFeedTimers()
      liveFeedContextRef.current = liveFeedContextKey
      liveLogsRef.current = incomingLogs
      setLiveFeedState({ logs: incomingLogs, enteringLogKeys: new Set() })
      return
    }

    const knownLogs = [...liveLogsRef.current, ...pendingLiveLogsRef.current]
    const discovered = mergeUsageLogLiveFeed(
      knownLogs,
      incomingLogs,
      knownLogs.length + incomingLogs.length
    )
    if (discovered.newItems.length === 0) return

    pendingLiveLogsRef.current.push(
      ...getLiveFeedInsertionOrder(discovered.newItems)
    )
    const nextInterval = getLiveFeedInsertInterval(
      pendingLiveLogsRef.current.length
    )
    if (nextInterval > 0) {
      liveInsertIntervalRef.current =
        liveInsertTimerRef.current && liveInsertIntervalRef.current > 0
          ? Math.min(liveInsertIntervalRef.current, nextInterval)
          : nextInterval
    }

    if (!isMobile && (liveTableBodyRef.current?.scrollTop ?? 0) > 2) {
      liveTableBodyRef.current?.scrollTo({
        top: 0,
        behavior: shouldReduceMotion ? 'auto' : 'smooth',
      })
    }

    if (!liveInsertTimerRef.current) {
      drainLiveFeedQueue()
    }
  }, [
    clearLiveFeedTimers,
    drainLiveFeedQueue,
    isAutoRefreshActive,
    isMobile,
    liveFeedContextKey,
    rawLogs,
    shouldReduceMotion,
  ])

  useEffect(() => () => clearLiveFeedTimers(), [clearLiveFeedTimers])

  useEffect(() => {
    if (!isAutoRefreshActive || isMobile) return

    const frame = requestAnimationFrame(() => {
      liveTableBodyRef.current?.scrollTo({ top: 0, behavior: 'auto' })
    })
    return () => cancelAnimationFrame(frame)
  }, [isAutoRefreshActive, isMobile, liveFeedContextKey])

  const logs =
    isAutoRefreshActive && liveFeedContextRef.current === liveFeedContextKey
      ? liveFeedState.logs
      : rawLogs
  const enteringLogKeys = liveFeedState.enteringLogKeys
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
      tableBodyClassName={isAutoRefreshActive ? '[overflow-anchor:none]' : ''}
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
        const rowClassName = cn('transition-colors duration-300', tintClass)

        if (
          isAutoRefreshActive &&
          !shouldReduceMotion &&
          row.index < LIVE_LAYOUT_ROW_LIMIT
        ) {
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
