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
 * Paginated raw error logs list.
 */
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Loader2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { CopyButton } from '@/components/copy-button'
import { formatCompactNumber, formatTimestamp } from '@/lib/format'
import { cn } from '@/lib/utils'
import { getErrorInsightLogs } from '../api'
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZE_OPTIONS,
} from '../constants'
import type { ErrorInsightFilterParams, ErrorInsightLog } from '../types'

interface LogsTableProps {
  params: ErrorInsightFilterParams
}

export function LogsTable(props: LogsTableProps) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState<number>(DEFAULT_PAGE_SIZE)

  const queryKey = useMemo(
    () =>
      [
        'error-insight',
        'logs',
        { ...props.params, page, page_size: pageSize },
      ] as const,
    [props.params, page, pageSize]
  )

  const {
    data: response,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey,
    queryFn: async () => {
      const result = await getErrorInsightLogs({
        ...props.params,
        page,
        page_size: pageSize,
      })
      if (!result.success) {
        throw new Error(result.message || t('Failed to load logs'))
      }
      return result.data
    },
  })

  const logs = response?.logs ?? []
  const total = response?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  // Clamp page when total shrinks (e.g. after a signature delete).
  const safePage = Math.min(page, totalPages)
  if (safePage !== page) setPage(safePage)

  const handlePageSizeChange = (value: string | null) => {
    if (!value) return
    setPageSize(Number(value))
    setPage(1)
  }

  return (
    <div className='bg-card ring-foreground/10 flex h-full min-h-0 flex-col overflow-hidden rounded-xl ring-1'>
      <div className='min-h-0 flex-1 overflow-auto'>
        {error ? (
          <ErrorState
            title={t('Failed to load')}
            description={error.message}
            onRetry={refetch}
            className='min-h-[240px]'
          />
        ) : isLoading && logs.length === 0 ? (
          <div className='flex min-h-[240px] flex-col items-center justify-center gap-3'>
            <Loader2 className='text-muted-foreground size-6 animate-spin' />
            <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
          </div>
        ) : logs.length === 0 ? (
          <EmptyState
            title={t('No logs found')}
            description={t('No error logs match the current filters.')}
            className='min-h-[240px]'
          />
        ) : (
          <Table>
            <TableHeader className='bg-muted/50 sticky top-0 z-10'>
              <TableRow>
                <TableHead className='w-[160px]'>{t('Time')}</TableHead>
                <TableHead className='min-w-[180px]'>{t('Request ID')}</TableHead>
                <TableHead className='w-[120px]'>{t('User')}</TableHead>
                <TableHead className='w-[100px] text-right'>
                  {t('Channel')}
                </TableHead>
                <TableHead className='w-[180px]'>{t('Model')}</TableHead>
                <TableHead className='w-[140px]'>{t('Rule Code')}</TableHead>
                <TableHead className='w-[110px]'>{t('Match Status')}</TableHead>
                <TableHead className='min-w-[220px]'>
                  {t('Safe Error')}
                </TableHead>
                <TableHead className='min-w-[220px]'>
                  {t('Original Error')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {logs.map((log) => (
                <LogRow key={log.id} log={log} />
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      <div className='flex flex-wrap items-center justify-between gap-2 border-t px-3 py-2'>
        <div className='text-muted-foreground flex items-center gap-2 text-xs'>
          <span>
            {t('Total: {{count}}', { count: formatCompactNumber(total) })}
          </span>
          <span aria-hidden>·</span>
          <Select value={String(pageSize)} onValueChange={handlePageSizeChange}>
            <SelectTrigger size='sm' className='h-7 w-[90px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PAGE_SIZE_OPTIONS.map((option) => (
                <SelectItem key={option} value={String(option)}>
                  {option} / {t('page').toLowerCase()}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className='flex items-center gap-1'>
          <Button
            variant='ghost'
            size='icon'
            className='size-7'
            disabled={page <= 1}
            onClick={() => setPage(1)}
            aria-label={t('First page')}
          >
            <ChevronsLeft className='size-4' />
          </Button>
          <Button
            variant='ghost'
            size='icon'
            className='size-7'
            disabled={page <= 1}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            aria-label={t('Previous')}
          >
            <ChevronLeft className='size-4' />
          </Button>
          <span className='text-muted-foreground px-2 text-xs tabular-nums'>
            {page} / {totalPages}
          </span>
          <Button
            variant='ghost'
            size='icon'
            className='size-7'
            disabled={page >= totalPages}
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            aria-label={t('Next')}
          >
            <ChevronRight className='size-4' />
          </Button>
          <Button
            variant='ghost'
            size='icon'
            className='size-7'
            disabled={page >= totalPages}
            onClick={() => setPage(totalPages)}
            aria-label={t('Last page')}
          >
            <ChevronsRight className='size-4' />
          </Button>
        </div>
      </div>
    </div>
  )
}

function LogRow({ log }: { log: ErrorInsightLog }) {
  const { t } = useTranslation()
  return (
    <TableRow>
      <TableCell className='text-muted-foreground whitespace-nowrap text-xs'>
        {formatTimestamp(log.created_at)}
      </TableCell>
      <TableCell>
        <div className='flex items-center gap-1.5'>
          <code className='bg-muted text-muted-foreground max-w-[180px] truncate rounded px-1.5 py-0.5 font-mono text-xs'>
            {log.request_id || '-'}
          </code>
          {log.request_id && (
            <CopyButton
              value={log.request_id}
              size='icon'
              variant='ghost'
              tooltip={t('Copy request ID')}
            />
          )}
        </div>
      </TableCell>
      <TableCell className='text-sm'>
        {log.username || (
          <span className='text-muted-foreground text-xs'>
            #{log.user_id}
          </span>
        )}
      </TableCell>
      <TableCell className='text-right tabular-nums'>
        {log.channel_id || (
          <span className='text-muted-foreground text-xs'>-</span>
        )}
      </TableCell>
      <TableCell>
        <span className='block max-w-[180px] truncate text-sm'>
          {log.model_name || '-'}
        </span>
      </TableCell>
      <TableCell>
        {log.rule_code ? (
          <Badge variant='secondary'>{log.rule_code}</Badge>
        ) : (
          <span className='text-muted-foreground text-xs'>-</span>
        )}
      </TableCell>
      <TableCell>
        <Badge
          variant='outline'
          className={cn(
            log.rule_matched
              ? 'border-emerald-500/40 text-emerald-600 dark:text-emerald-400'
              : 'border-rose-500/40 text-rose-600 dark:text-rose-400'
          )}
        >
          {log.rule_matched ? t('Matched') : t('Unmatched')}
        </Badge>
      </TableCell>
      <TableCell>
        <div className='flex flex-col gap-0.5'>
          <span className='line-clamp-2 text-xs'>
            {log.safe_error_message || '-'}
          </span>
          {(log.safe_error_code || log.safe_error_type) && (
            <span className='text-muted-foreground text-xs'>
              {log.safe_error_code}
              {log.safe_error_code && log.safe_error_type ? ' · ' : ''}
              {log.safe_error_type}
            </span>
          )}
        </div>
      </TableCell>
      <TableCell>
        <div className='flex flex-col gap-0.5'>
          <span className='line-clamp-2 text-xs'>
            {log.original_error_message || '-'}
          </span>
          {(log.original_error_code || log.original_error_type) && (
            <span className='text-muted-foreground text-xs'>
              {log.original_error_code}
              {log.original_error_code && log.original_error_type
                ? ' · '
                : ''}
              {log.original_error_type}
            </span>
          )}
        </div>
      </TableCell>
    </TableRow>
  )
}
