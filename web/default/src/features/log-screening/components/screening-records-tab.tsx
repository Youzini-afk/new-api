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
 * Screening records tab: filters, paginated table, run / cleanup actions,
 * run summary dialog, and a record detail dialog (with suspicious IP marks).
 */
import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  Ban,
  Eraser,
  Eye,
  Loader2,
  PlayCircle,
  ShieldAlert,
} from 'lucide-react'
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  cleanupLogScreeningRecords,
  getLogScreeningRecords,
  runLogScreening,
  appendScreeningRemark,
} from '../api'
import {
  DEFAULT_PAGE_SIZE,
  DEFAULT_SCREENING_KIND,
  PAGE_SIZE_OPTIONS,
  SCREENING_KINDS,
  TRISTATE_OPTIONS,
  getRiskLevelConfig,
  tristateToQuery,
  type ScreeningKind,
  type TristateFilter,
} from '../constants'
import type { LogScreeningRecordItem, LogScreeningRunSummary } from '../types'
import {
  formatScreeningNumber,
  formatScreeningTimestamp,
} from '../lib/utils'
import { ListPagination } from './list-pagination'
import { RemarkDialog } from './remark-dialog'
import { RunSummaryDialog } from './run-summary-dialog'

interface RecordsFilter {
  username: string
  ip: string
  rule: string
  window: string
  param_key: string
  ua: string
  request_path: string
  expired: TristateFilter
}

const EMPTY_FILTER: RecordsFilter = {
  username: '',
  ip: '',
  rule: '',
  window: '',
  param_key: '',
  ua: '',
  request_path: '',
  expired: 'any',
}

export function ScreeningRecordsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [filter, setFilter] = useState<RecordsFilter>(EMPTY_FILTER)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [runKind, setRunKind] = useState<ScreeningKind>(DEFAULT_SCREENING_KIND)
  const [remarkTarget, setRemarkTarget] =
    useState<LogScreeningRecordItem | null>(null)
  const [detailTarget, setDetailTarget] =
    useState<LogScreeningRecordItem | null>(null)
  const [runSummary, setRunSummary] = useState<LogScreeningRunSummary | null>(
    null
  )

  const queryParams = useMemo(
    () => ({
      p: page,
      page_size: pageSize,
      username: filter.username.trim() || undefined,
      ip: filter.ip.trim() || undefined,
      rule: filter.rule.trim() || undefined,
      window: filter.window.trim() || undefined,
      param_key: filter.param_key.trim() || undefined,
      ua: filter.ua.trim() || undefined,
      request_path: filter.request_path.trim() || undefined,
      expired: tristateToQuery(filter.expired),
    }),
    [filter, page, pageSize]
  )

  const {
    data: response,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: ['log-screening', 'records', queryParams],
    queryFn: async () => {
      const result = await getLogScreeningRecords(queryParams)
      if (!result.success) {
        throw new Error(result.message || t('Failed to load screening records'))
      }
      return result.data
    },
  })

  const runMutation = useMutation({
    mutationFn: (kind: ScreeningKind) => runLogScreening(kind),
    onSuccess: (data) => {
      if (data.success && data.data) {
        setRunSummary(data.data)
        toast.success(t('Screening run completed'))
        void queryClient.invalidateQueries({
          queryKey: ['log-screening', 'records'],
        })
      } else {
        toast.error(data.message || t('Screening run failed'))
      }
    },
    onError: (err: Error) => {
      toast.error(err.message || t('Screening run failed'))
    },
  })

  const cleanupMutation = useMutation({
    mutationFn: () => cleanupLogScreeningRecords(),
    onSuccess: (data) => {
      if (data.success && data.data) {
        toast.success(
          t('Cleaned up {{count}} expired records', {
            count: data.data.deleted,
          })
        )
        void queryClient.invalidateQueries({
          queryKey: ['log-screening', 'records'],
        })
      } else {
        toast.error(data.message || t('Cleanup failed'))
      }
    },
    onError: (err: Error) => {
      toast.error(err.message || t('Cleanup failed'))
    },
  })

  const items = response?.items ?? []
  const total = response?.total ?? 0

  const applyFilter = () => {
    setPage(1)
  }

  const resetFilter = () => {
    setFilter(EMPTY_FILTER)
    setPage(1)
  }

  const renderRecordsBody = () => {
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
          title={t('No screening records')}
          description={t(
            'Run a screening pass or adjust filters to see records.'
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
            <TableHead>{t('Risk')}</TableHead>
            <TableHead>{t('Rule')}</TableHead>
            <TableHead className='text-right'>{t('Count')}</TableHead>
            <TableHead className='text-right'>{t('RPM')}</TableHead>
            <TableHead className='text-right'>{t('TPM')}</TableHead>
            <TableHead>{t('IP')}</TableHead>
            <TableHead>{t('Window')}</TableHead>
            <TableHead>{t('Matched at')}</TableHead>
            <TableHead>{t('Status')}</TableHead>
            <TableHead className='text-right'>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((item) => (
            <RecordsTableRow
              key={item.id}
              item={item}
              onView={() => setDetailTarget(item)}
              onRemark={() => setRemarkTarget(item)}
            />
          ))}
        </TableBody>
      </Table>
    )
  }

  const expiredLabel =
    TRISTATE_OPTIONS.find((opt) => opt.value === filter.expired)?.labelKey ??
    filter.expired
  const runKindLabel =
    SCREENING_KINDS.find((k) => k.value === runKind)?.labelKey ?? runKind

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
            value={filter.rule}
            onChange={(v) => setFilter({ ...filter, rule: v })}
            placeholder={t('Rule')}
          />
          <FilterInput
            value={filter.window}
            onChange={(v) => setFilter({ ...filter, window: v })}
            placeholder={t('Window')}
          />
          <FilterInput
            value={filter.param_key}
            onChange={(v) => setFilter({ ...filter, param_key: v })}
            placeholder={t('Param key')}
          />
          <FilterInput
            value={filter.ua}
            onChange={(v) => setFilter({ ...filter, ua: v })}
            placeholder={t('UA')}
          />
          <FilterInput
            value={filter.request_path}
            onChange={(v) => setFilter({ ...filter, request_path: v })}
            placeholder={t('Request path')}
          />
          <Select
            value={filter.expired}
            onValueChange={(v) =>
              setFilter({ ...filter, expired: v as TristateFilter })
            }
          >
            <SelectTrigger className='w-[130px]'>
              <SelectValue>{t(expiredLabel)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {TRISTATE_OPTIONS.map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>
                  {t(opt.labelKey)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant='secondary' size='sm' onClick={applyFilter}>
            {t('Filter')}
          </Button>
          <Button variant='ghost' size='sm' onClick={resetFilter}>
            {t('Reset')}
          </Button>
        </div>

        <div className='flex flex-wrap items-center gap-2'>
          <Select
            value={runKind}
            onValueChange={(v) => setRunKind(v as ScreeningKind)}
          >
            <SelectTrigger className='w-[180px]'>
              <SelectValue>{t(runKindLabel)}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {SCREENING_KINDS.map((k) => (
                <SelectItem key={k.value} value={k.value}>
                  {t(k.labelKey)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            size='sm'
            onClick={() => runMutation.mutate(runKind)}
            disabled={runMutation.isPending}
          >
            {runMutation.isPending ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <PlayCircle className='size-4' />
            )}
            {t('Run screening')}
          </Button>
          <Button
            variant='destructive'
            size='sm'
            onClick={() => cleanupMutation.mutate()}
            disabled={cleanupMutation.isPending}
          >
            {cleanupMutation.isPending ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Eraser className='size-4' />
            )}
            {t('Cleanup expired')}
          </Button>
        </div>
      </div>

      <div className='bg-card ring-foreground/10 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl ring-1'>
        <div className='min-h-0 flex-1 overflow-auto'>{renderRecordsBody()}</div>
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

      <RemarkDialog
        open={remarkTarget !== null}
        onOpenChange={(open) => {
          if (!open) setRemarkTarget(null)
        }}
        title={t('Append remark')}
        description={
          remarkTarget
            ? t('Append remark to user {{name}}', {
                name: remarkTarget.username || `ID: ${remarkTarget.user_id}`,
              })
            : ''
        }
        currentRemark={remarkTarget?.remark}
        submit={(remark) =>
          appendScreeningRemark(remarkTarget?.id ?? 0, remark)
        }
        invalidateQueries={[
          ['log-screening', 'records'],
          ['log-screening', 'record', remarkTarget?.id],
        ]}
      />

      <ScreeningRecordDetailDialog
        item={detailTarget}
        open={detailTarget !== null}
        onOpenChange={(open) => {
          if (!open) setDetailTarget(null)
        }}
        onRemark={() => {
          if (detailTarget) {
            setRemarkTarget(detailTarget)
            setDetailTarget(null)
          }
        }}
      />

      <RunSummaryDialog
        summary={runSummary}
        open={runSummary !== null}
        onOpenChange={(open) => {
          if (!open) setRunSummary(null)
        }}
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

function RecordsTableRow(props: {
  item: LogScreeningRecordItem
  onView: () => void
  onRemark: () => void
}) {
  const { t } = useTranslation()
  const { item } = props
  const risk = getRiskLevelConfig(item.risk_level)
  const expired = item.expires_at > 0 && item.expires_at <= Date.now() / 1000

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
        <Badge variant='outline' className={risk.badgeClass}>
          {t(risk.labelKey)}
        </Badge>
      </TableCell>
      <TableCell className='text-sm'>{item.rule_name || '-'}</TableCell>
      <TableCell className='text-right tabular-nums'>
        {formatScreeningNumber(item.request_count)}
      </TableCell>
      <TableCell className='text-right tabular-nums'>
        {formatScreeningNumber(item.rpm)}
      </TableCell>
      <TableCell className='text-right tabular-nums'>
        {formatScreeningNumber(item.tpm)}
      </TableCell>
      <TableCell>
        {item.ip ? (
          <span className='font-mono text-xs'>{item.ip}</span>
        ) : (
          '-'
        )}
      </TableCell>
      <TableCell className='text-muted-foreground text-xs'>
        {item.window || '-'}
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
      <TableCell>
        <Badge variant={expired ? 'secondary' : 'default'}>
          {expired ? t('Expired') : t('Active')}
        </Badge>
      </TableCell>
      <TableCell className='text-right'>
        <div className='flex items-center justify-end gap-1'>
          {item.suspicious_ips.length > 0 && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Badge variant='destructive' className='gap-1'>
                    <ShieldAlert className='size-3' />
                    {item.suspicious_ips.length}
                  </Badge>
                }
              />
              <TooltipContent className='max-w-sm'>
                {item.suspicious_ips
                  .map((m) => m.ip)
                  .filter(Boolean)
                  .join(', ')}
              </TooltipContent>
            </Tooltip>
          )}
          <Button variant='ghost' size='icon-sm' onClick={props.onView}>
            <Eye className='size-4' />
          </Button>
          <Button variant='ghost' size='icon-sm' onClick={props.onRemark}>
            <Ban className='size-4' />
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}

function ScreeningRecordDetailDialog(props: {
  item: LogScreeningRecordItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onRemark: () => void
}) {
  const { t } = useTranslation()
  const { item } = props

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Screening Record Detail')}</DialogTitle>
        </DialogHeader>
        {item && (
          <div className='max-h-[60vh] overflow-auto'>
            <div className='grid grid-cols-2 gap-3 text-sm'>
              <DetailField label={t('User')}>
                {item.username || `ID: ${item.user_id}`}
              </DetailField>
              <DetailField label={t('Rule')}>
                {item.rule_name || '-'}
              </DetailField>
              <DetailField label={t('Window')}>
                {item.window || '-'}
              </DetailField>
              <DetailField label={t('Request path')} mono>
                {item.request_path || '-'}
              </DetailField>
              <DetailField label={t('Window start')}>
                {formatScreeningTimestamp(item.window_start)}
              </DetailField>
              <DetailField label={t('Window end')}>
                {formatScreeningTimestamp(item.window_end)}
              </DetailField>
              <DetailField label={t('Request count')}>
                {formatScreeningNumber(item.request_count)}
              </DetailField>
              <DetailField label={t('RPM')}>
                {formatScreeningNumber(item.rpm)}
              </DetailField>
              <DetailField label={t('RPH')}>
                {formatScreeningNumber(item.rph)}
              </DetailField>
              <DetailField label={t('TPM')}>
                {formatScreeningNumber(item.tpm)}
              </DetailField>
              <DetailField label={t('Prompt delta count')}>
                {formatScreeningNumber(item.prompt_delta_count)}
              </DetailField>
              <DetailField label={t('Prompt delta max')}>
                {formatScreeningNumber(item.prompt_delta_max)}
              </DetailField>
              <DetailField label={t('Matched at')}>
                {formatScreeningTimestamp(item.matched_at)}
              </DetailField>
              <DetailField label={t('Expires at')}>
                {formatScreeningTimestamp(item.expires_at)}
              </DetailField>
              <DetailField label={t('IP')} mono>
                {item.ip || '-'}
              </DetailField>
              <DetailField label={t('Token')}>
                {item.token_name || '-'}
              </DetailField>
              <DetailField label={t('Operator')}>
                {item.operator_name || '-'}
              </DetailField>
              <DetailField label={t('Manual triggered')}>
                {item.manual_triggered ? t('Yes') : t('No')}
              </DetailField>
            </div>

            <div className='mt-4'>
              <p className='text-muted-foreground mb-1 text-xs'>
                {t('Param hits')}
              </p>
              <HitList hits={item.param_hits} emptyText={t('None')} />
            </div>

            <div className='mt-3'>
              <p className='text-muted-foreground mb-1 text-xs'>
                {t('UA hits')}
              </p>
              <HitList hits={item.ua_hits} emptyText={t('None')} />
            </div>

            <div className='mt-3'>
              <p className='text-muted-foreground mb-1 text-xs'>
                {t('Suspicious IP marks')}
              </p>
              {item.suspicious_ips.length === 0 ? (
                <p className='text-sm'>{t('None')}</p>
              ) : (
                <div className='flex flex-col gap-2'>
                  {item.suspicious_ips.map((m, idx) => (
                    <div
                      key={idx}
                      className='bg-muted/40 rounded-lg border p-2 text-xs'
                    >
                      <div className='flex items-center justify-between gap-2'>
                        <span className='font-mono'>{m.ip || '-'}</span>
                        <Badge variant='outline'>{m.source}</Badge>
                      </div>
                      {m.ban_reason && (
                        <p className='text-muted-foreground mt-1 break-words'>
                          {m.ban_reason}
                        </p>
                      )}
                      {m.context && (
                        <p className='text-muted-foreground mt-1 break-words'>
                          {m.context}
                        </p>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>

            {item.remark && (
              <div className='mt-3'>
                <p className='text-muted-foreground mb-1 text-xs'>
                  {t('Current remark')}
                </p>
                <p className='bg-muted/40 rounded-lg border p-2 text-xs whitespace-pre-wrap break-words'>
                  {item.remark}
                </p>
              </div>
            )}

            <div className='mt-4 flex justify-end'>
              <Button variant='secondary' size='sm' onClick={props.onRemark}>
                <Ban className='size-4' />
                {t('Append remark')}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

function DetailField(props: {
  label: string
  mono?: boolean
  children: React.ReactNode
}) {
  return (
    <div className='flex flex-col gap-0.5'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className={cn('break-words', props.mono && 'font-mono text-xs')}>
        {props.children}
      </span>
    </div>
  )
}

function HitList(props: { hits: string[]; emptyText: string }) {
  if (!props.hits || props.hits.length === 0) {
    return <p className='text-sm'>{props.emptyText}</p>
  }
  return (
    <div className='flex flex-wrap gap-1'>
      {props.hits.map((h, idx) => (
        <Badge key={idx} variant='secondary' className='font-mono'>
          {h}
        </Badge>
      ))}
    </div>
  )
}
