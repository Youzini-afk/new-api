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
 * Shared block-logs list tab used by Prompt Blocks and UA Blocks. Each kind
 * supplies its API functions and the extra row affordances (e.g. UA badge).
 */
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Eye, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
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
import { Badge } from '@/components/ui/badge'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { CopyButton } from '@/components/copy-button'
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZE_OPTIONS,
  TRISTATE_OPTIONS,
  tristateToQuery,
  type TristateFilter,
} from '../constants'
import { formatScreeningTimestamp } from '../lib/utils'
import { ListPagination } from './list-pagination'
import { RemarkDialog } from './remark-dialog'
import {
  BlockLogDetailDrawer,
  type BlockLogDetail,
  type BlockLogKind,
} from './block-log-detail-drawer'

interface BlockLogsTabProps {
  kind: BlockLogKind
  /** Fetch a page of list items. */
  list: (
    params: Record<string, unknown>
  ) => Promise<{
    success: boolean
    message?: string
    data?: { page: number; page_size: number; total: number; items: unknown[] }
  }>
  /** Fetch a single detail (with raw headers/params). */
  detail: (id: number) => Promise<{
    success: boolean
    message?: string
    data?: BlockLogDetail
  }>
  /** POST a remark append. */
  remark: (
    id: number,
    remark: string
  ) => Promise<{ success: boolean; message?: string }>
  queryKeyPrefix: string
}

interface BlockLogFilter {
  username: string
  ip: string
  rule_pattern: string
  request_path: string
  error_code: string
  match_mode: string
  auto_banned: TristateFilter
  is_empty_ua: TristateFilter
}

const EMPTY_FILTER: BlockLogFilter = {
  username: '',
  ip: '',
  rule_pattern: '',
  request_path: '',
  error_code: '',
  match_mode: '',
  auto_banned: 'any',
  is_empty_ua: 'any',
}

export function BlockLogsTab(props: BlockLogsTabProps) {
  const { t } = useTranslation()
  const isUa = props.kind === 'ua'

  const [filter, setFilter] = useState<BlockLogFilter>(EMPTY_FILTER)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [detailId, setDetailId] = useState<number | null>(null)
  const [remarkItem, setRemarkItem] = useState<{
    id: number
    username?: string
    remark?: string
  } | null>(null)

  const queryParams = useMemo(
    () => ({
      p: page,
      page_size: pageSize,
      username: filter.username.trim() || undefined,
      ip: filter.ip.trim() || undefined,
      rule_pattern: filter.rule_pattern.trim() || undefined,
      request_path: filter.request_path.trim() || undefined,
      error_code: filter.error_code.trim() || undefined,
      match_mode: !isUa ? filter.match_mode.trim() || undefined : undefined,
      auto_banned: tristateToQuery(filter.auto_banned),
      is_empty_ua: isUa ? tristateToQuery(filter.is_empty_ua) : undefined,
    }),
    [filter, page, pageSize, isUa]
  )

  const {
    data: response,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: ['log-screening', props.queryKeyPrefix, queryParams],
    queryFn: async () => {
      const result = await props.list(queryParams)
      if (!result.success) {
        throw new Error(result.message || t('Failed to load records'))
      }
      return result.data
    },
  })

  const detailQuery = useQuery({
    queryKey: ['log-screening', props.queryKeyPrefix, 'detail', detailId],
    queryFn: async () => {
      if (detailId == null) return null
      const result = await props.detail(detailId)
      if (!result.success) {
        throw new Error(result.message || t('Failed to load detail'))
      }
      return result.data ?? null
    },
    enabled: detailId != null,
  })

  const items = (response?.items ?? []) as Array<{
    id: number
    user_id: number
    username: string
    display_name: string
    remark: string
    ip: string
    rule_pattern: string
    error_code: string
    http_status_code: number
    request_path: string
    match_mode: string
    user_agent: string
    is_empty_ua: boolean
    auto_ban_configured: boolean
    auto_banned: boolean
    matched_at: number
  }>

  const total = response?.total ?? 0

  const renderBody = () => {
    if (isLoading && items.length === 0) {
      return (
        <div className='flex min-h-[240px] flex-col items-center justify-center gap-3'>
          <Loader2 className='text-muted-foreground size-6 animate-spin' />
          <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
        </div>
      )
    }
    if (error) {
      return (
        <ErrorState
          title={t('Failed to load')}
          description={error.message}
          onRetry={refetch}
          className='min-h-[240px]'
        />
      )
    }
    if (items.length === 0) {
      return (
        <EmptyState
          title={t('No records found')}
          description={t(
            'No interception records match the current filters.'
          )}
          className='min-h-[240px]'
        />
      )
    }
    return (
      <Table>
        <TableHeader className='bg-muted/50 sticky top-0 z-10'>
          <TableRow>
            <TableHead>{t('User')}</TableHead>
            <TableHead>{t('IP')}</TableHead>
            <TableHead>{t('Rule')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead>{t('Path')}</TableHead>
            {isUa && <TableHead>{t('User Agent')}</TableHead>}
            <TableHead>{t('Auto-ban')}</TableHead>
            <TableHead>{t('Matched at')}</TableHead>
            <TableHead className='text-right'>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((item) => (
            <BlockLogRow
              key={item.id}
              kind={props.kind}
              item={item}
              onView={() => setDetailId(item.id)}
              onRemark={() =>
                setRemarkItem({
                  id: item.id,
                  username: item.username,
                  remark: item.remark,
                })
              }
            />
          ))}
        </TableBody>
      </Table>
    )
  }

  const autoBannedLabel =
    TRISTATE_OPTIONS.find((opt) => opt.value === filter.auto_banned)?.labelKey ??
    filter.auto_banned
  const emptyUALabel =
    TRISTATE_OPTIONS.find((opt) => opt.value === filter.is_empty_ua)?.labelKey ??
    filter.is_empty_ua

  return (
    <div className='flex h-full min-h-0 flex-col gap-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <div className='flex flex-wrap items-center gap-2'>
          <FilterInput
            value={filter.username}
            onChange={(v) => setFilter({ ...filter, username: v })}
            placeholder={t('Username')}
          />
          <FilterInput
            value={filter.ip}
            onChange={(v) => setFilter({ ...filter, ip: v })}
            placeholder={t('IP')}
          />
          <FilterInput
            value={filter.rule_pattern}
            onChange={(v) => setFilter({ ...filter, rule_pattern: v })}
            placeholder={t('Rule pattern')}
          />
          <FilterInput
            value={filter.request_path}
            onChange={(v) => setFilter({ ...filter, request_path: v })}
            placeholder={t('Request path')}
          />
          <FilterInput
            value={filter.error_code}
            onChange={(v) => setFilter({ ...filter, error_code: v })}
            placeholder={t('Error code')}
          />
          {!isUa && (
            <FilterInput
              value={filter.match_mode}
              onChange={(v) => setFilter({ ...filter, match_mode: v })}
              placeholder={t('Match mode')}
            />
          )}
          <Select
            value={filter.auto_banned}
            onValueChange={(v) =>
              setFilter({ ...filter, auto_banned: v as TristateFilter })
            }
          >
            <SelectTrigger className='w-[150px]'>
              <SelectValue>
                {t('Auto-banned')}: {t(autoBannedLabel)}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {TRISTATE_OPTIONS.map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>
                  {t('Auto-banned')}: {t(opt.labelKey)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {isUa && (
            <Select
              value={filter.is_empty_ua}
              onValueChange={(v) =>
                setFilter({ ...filter, is_empty_ua: v as TristateFilter })
              }
            >
              <SelectTrigger className='w-[150px]'>
                <SelectValue>
                  {t('Empty UA')}: {t(emptyUALabel)}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {TRISTATE_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {t('Empty UA')}: {t(opt.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <Button
            variant='ghost'
            size='sm'
            onClick={() => {
              setFilter(EMPTY_FILTER)
              setPage(1)
            }}
          >
            {t('Reset')}
          </Button>
        </div>
      </div>

      <div className='bg-card ring-foreground/10 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl ring-1'>
        <div className='min-h-0 flex-1 overflow-auto'>{renderBody()}</div>
      </div>

      <div className='flex flex-wrap items-center justify-between gap-2'>
        <Select
          value={String(pageSize)}
          onValueChange={(v) => {
            setPageSize(Number(v))
            setPage(1)
          }}
        >
          <SelectTrigger className='w-[110px]'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PAGE_SIZE_OPTIONS.map((opt) => (
              <SelectItem key={opt} value={String(opt)}>
                {opt} / {t('page')}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ListPagination
          page={page}
          pageSize={pageSize}
          total={total}
          onPageChange={setPage}
        />
      </div>

      <BlockLogDetailDrawer
        kind={props.kind}
        open={detailId != null}
        onOpenChange={(open) => {
          if (!open) setDetailId(null)
        }}
        detail={detailQuery.data ?? null}
        isLoading={detailQuery.isLoading}
        onAddRemark={() => {
          const detail = detailQuery.data
          if (detail) {
            setRemarkItem({
              id: detail.id,
              username: detail.username,
              remark: detail.remark,
            })
            setDetailId(null)
          }
        }}
      />

      <RemarkDialog
        open={remarkItem !== null}
        onOpenChange={(open) => {
          if (!open) setRemarkItem(null)
        }}
        title={t('Append remark')}
        description={
          remarkItem
            ? t('Append remark to user {{name}}', {
                name: remarkItem.username || `ID`,
              })
            : ''
        }
        currentRemark={remarkItem?.remark}
        submit={(remark) => props.remark(remarkItem?.id ?? 0, remark)}
        invalidateQueries={[
          ['log-screening', props.queryKeyPrefix],
          ['log-screening', props.queryKeyPrefix, 'detail', remarkItem?.id],
        ]}
      />
    </div>
  )
}

function FilterInput(props: {
  value: string
  onChange: (value: string) => void
  placeholder: string
}) {
  return (
    <Input
      value={props.value}
      onChange={(e) => props.onChange(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === 'Enter') e.preventDefault()
      }}
      placeholder={props.placeholder}
      className='w-[140px]'
    />
  )
}

interface BlockLogRowItem {
  id: number
  user_id: number
  username: string
  display_name: string
  remark: string
  ip: string
  rule_pattern: string
  error_code: string
  http_status_code: number
  request_path: string
  match_mode: string
  user_agent: string
  is_empty_ua: boolean
  auto_ban_configured: boolean
  auto_banned: boolean
  matched_at: number
}

function BlockLogRow(props: {
  kind: BlockLogKind
  item: BlockLogRowItem
  onView: () => void
  onRemark: () => void
}) {
  const { t } = useTranslation()
  const { item } = props
  const isUa = props.kind === 'ua'

  const renderUaCell = () => {
    if (item.is_empty_ua) {
      return <Badge variant='destructive'>{t('Empty UA')}</Badge>
    }
    if (item.user_agent) {
      return (
        <Tooltip>
          <TooltipTrigger
            render={
              <span className='block max-w-[200px] truncate font-mono text-xs'>
                {item.user_agent}
              </span>
            }
          />
          <TooltipContent className='max-w-sm break-all'>
            {item.user_agent}
          </TooltipContent>
        </Tooltip>
      )
    }
    return <>-</>
  }

  return (
    <TableRow>
      <TableCell>
        <div className='flex flex-col'>
          <span className='text-sm font-medium'>
            {item.username || `ID: ${item.user_id}`}
          </span>
          {item.display_name && (
            <span className='text-muted-foreground text-xs'>
              {item.display_name}
            </span>
          )}
        </div>
      </TableCell>
      <TableCell>
        {item.ip ? (
          <span className='font-mono text-xs'>{item.ip}</span>
        ) : (
          '-'
        )}
      </TableCell>
      <TableCell className='text-sm'>
        {item.rule_pattern || '-'}
      </TableCell>
      <TableCell>
        <Badge variant='outline' className='font-mono'>
          {item.http_status_code || '-'}
        </Badge>
        {item.error_code && (
          <span className='text-muted-foreground ml-1 text-xs'>
            {item.error_code}
          </span>
        )}
      </TableCell>
      <TableCell className='text-muted-foreground text-xs'>
        {item.request_path || '-'}
      </TableCell>
      {isUa && <TableCell>{renderUaCell()}</TableCell>}
      <TableCell>
        {item.auto_ban_configured ? (
          <Badge variant={item.auto_banned ? 'destructive' : 'secondary'}>
            {item.auto_banned ? t('Banned') : t('Configured')}
          </Badge>
        ) : (
          <span className='text-muted-foreground text-xs'>{t('Off')}</span>
        )}
      </TableCell>
      <TableCell className='text-muted-foreground text-xs'>
        {formatScreeningTimestamp(item.matched_at, {
          year: undefined,
          month: '2-digit',
          day: '2-digit',
          hour: '2-digit',
          minute: '2-digit',
        })}
      </TableCell>
      <TableCell className='text-right'>
        <div className='flex items-center justify-end gap-1'>
          <Button variant='ghost' size='icon-sm' onClick={props.onView}>
            <Eye className='size-4' />
          </Button>
          {item.user_agent && isUa && (
            <CopyButton
              value={item.user_agent}
              size='icon'
              variant='ghost'
              tooltip={t('Copy User Agent')}
            />
          )}
        </div>
      </TableCell>
    </TableRow>
  )
}
