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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Eye, Loader2, PlayCircle, RefreshCw, ShieldAlert } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
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
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { getRiskCases, runRiskControl } from '../api'
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZE_OPTIONS,
  getRiskLevelConfig,
} from '../constants'
import {
  actionLabel,
  parseRiskSignals,
  statusLabel,
  verdictLabel,
} from '../lib/risk-control'
import { formatScreeningTimestamp } from '../lib/utils'
import type { RiskCaseItem, RiskCaseListParams } from '../types'
import { ListPagination } from './list-pagination'
import { RiskCaseDetailDrawer } from './risk-case-detail-drawer'

interface RiskCaseFilters {
  userId: string
  tokenId: string
  status: string
  verdict: string
  riskLevel: string
  minScore: string
}

const EMPTY_FILTERS: RiskCaseFilters = {
  userId: '',
  tokenId: '',
  status: 'all',
  verdict: 'all',
  riskLevel: 'all',
  minScore: '',
}

const CASE_STATUSES = ['open', 'reviewing', 'actioned', 'resolved', 'dismissed']
const CASE_VERDICTS = [
  'normal',
  'small_share',
  'key_leak',
  'gateway_distribution',
  'multi_node_gateway',
  'commercial_resale',
  'forbidden_paid_client',
  'uncertain',
]

export function RiskCasesTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const role = useAuthStore((state) => state.auth.user?.role ?? ROLE.GUEST)
  const [draftFilters, setDraftFilters] =
    useState<RiskCaseFilters>(EMPTY_FILTERS)
  const [filters, setFilters] = useState<RiskCaseFilters>(EMPTY_FILTERS)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [detailId, setDetailId] = useState<number | null>(null)

  const queryParams = useMemo<RiskCaseListParams>(() => {
    const userId = Number(filters.userId)
    const tokenId = Number(filters.tokenId)
    const minScore = Number(filters.minScore)
    return {
      p: page,
      page_size: pageSize,
      user_id: userId > 0 ? userId : undefined,
      token_id: tokenId > 0 ? tokenId : undefined,
      status: filters.status === 'all' ? undefined : filters.status,
      verdict: filters.verdict === 'all' ? undefined : filters.verdict,
      risk_level: filters.riskLevel === 'all' ? undefined : filters.riskLevel,
      min_score: minScore > 0 ? minScore : undefined,
    }
  }, [filters, page, pageSize])

  const casesQuery = useQuery({
    queryKey: ['risk-control', 'cases', queryParams],
    queryFn: async () => {
      const result = await getRiskCases(queryParams)
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load risk cases'))
      }
      return result.data
    },
  })

  const runMutation = useMutation({
    mutationFn: runRiskControl,
    onSuccess: (result) => {
      if (!result.success || !result.data) {
        toast.error(result.message || t('Failed to start risk screening'))
        return
      }
      toast.success(
        result.data.created
          ? t('Risk screening task queued')
          : t('A risk screening task is already running')
      )
      void queryClient.invalidateQueries({
        queryKey: ['risk-control', 'cases'],
      })
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to start risk screening'))
    },
  })

  const items = casesQuery.data?.items ?? []
  const total = casesQuery.data?.total ?? 0

  const applyFilters = () => {
    setFilters(draftFilters)
    setPage(1)
  }

  const resetFilters = () => {
    setDraftFilters(EMPTY_FILTERS)
    setFilters(EMPTY_FILTERS)
    setPage(1)
  }

  let casesContent = <RiskCasesTable items={items} onOpen={setDetailId} />
  if (casesQuery.isLoading && items.length === 0) {
    casesContent = (
      <div className='flex min-h-60 items-center justify-center'>
        <Loader2 className='text-muted-foreground size-6 animate-spin' />
      </div>
    )
  } else if (casesQuery.error) {
    casesContent = (
      <ErrorState
        title={t('Failed to load')}
        description={casesQuery.error.message}
        onRetry={() => casesQuery.refetch()}
        className='min-h-60'
      />
    )
  } else if (items.length === 0) {
    casesContent = (
      <EmptyState
        icon={ShieldAlert}
        title={t('No risk cases')}
        description={t(
          'Run risk screening after enabling it, or adjust the filters.'
        )}
        className='min-h-60'
      />
    )
  }

  return (
    <div className='flex h-full min-h-0 flex-col gap-4'>
      <div className='flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between'>
        <div className='flex flex-wrap items-center gap-2'>
          <Input
            value={draftFilters.userId}
            onChange={(event) =>
              setDraftFilters({ ...draftFilters, userId: event.target.value })
            }
            placeholder={t('User ID')}
            className='w-28'
          />
          <Input
            value={draftFilters.tokenId}
            onChange={(event) =>
              setDraftFilters({ ...draftFilters, tokenId: event.target.value })
            }
            placeholder={t('Token ID')}
            className='w-28'
          />
          <FilterSelect
            value={draftFilters.status}
            placeholder={t('Status')}
            options={CASE_STATUSES}
            onChange={(status) => setDraftFilters({ ...draftFilters, status })}
          />
          <FilterSelect
            value={draftFilters.verdict}
            placeholder={t('Verdict')}
            options={CASE_VERDICTS}
            onChange={(verdict) =>
              setDraftFilters({ ...draftFilters, verdict })
            }
          />
          <FilterSelect
            value={draftFilters.riskLevel}
            placeholder={t('Risk level')}
            options={['critical', 'high', 'medium', 'low']}
            onChange={(riskLevel) =>
              setDraftFilters({ ...draftFilters, riskLevel })
            }
          />
          <Input
            type='number'
            min={0}
            max={100}
            value={draftFilters.minScore}
            onChange={(event) =>
              setDraftFilters({ ...draftFilters, minScore: event.target.value })
            }
            placeholder={t('Min score')}
            className='w-28'
          />
          <Button size='sm' variant='secondary' onClick={applyFilters}>
            {t('Filter')}
          </Button>
          <Button size='sm' variant='ghost' onClick={resetFilters}>
            {t('Reset')}
          </Button>
        </div>

        <div className='flex items-center gap-2'>
          <Button
            size='sm'
            variant='outline'
            onClick={() => casesQuery.refetch()}
            disabled={casesQuery.isFetching}
          >
            <RefreshCw
              className={
                casesQuery.isFetching ? 'size-4 animate-spin' : 'size-4'
              }
            />
            {t('Refresh')}
          </Button>
          {role >= ROLE.SUPER_ADMIN && (
            <Button
              size='sm'
              onClick={() => runMutation.mutate()}
              disabled={runMutation.isPending}
            >
              {runMutation.isPending ? (
                <Loader2 className='size-4 animate-spin' />
              ) : (
                <PlayCircle className='size-4' />
              )}
              {t('Run risk screening')}
            </Button>
          )}
        </div>
      </div>

      <div className='bg-card ring-foreground/10 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl ring-1'>
        <div className='min-h-0 flex-1 overflow-auto'>{casesContent}</div>
      </div>

      <div className='flex flex-wrap items-center justify-between gap-2'>
        <Select
          value={String(pageSize)}
          onValueChange={(value) => {
            setPageSize(Number(value))
            setPage(1)
          }}
        >
          <SelectTrigger className='w-28'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PAGE_SIZE_OPTIONS.map((option) => (
              <SelectItem key={option} value={String(option)}>
                {option} / {t('page')}
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

      <RiskCaseDetailDrawer
        caseId={detailId}
        open={detailId !== null}
        onOpenChange={(open) => {
          if (!open) setDetailId(null)
        }}
      />
    </div>
  )
}

function RiskCasesTable(props: {
  items: RiskCaseItem[]
  onOpen: (id: number) => void
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader className='bg-muted/50 sticky top-0 z-10'>
        <TableRow>
          <TableHead>{t('User / Token')}</TableHead>
          <TableHead>{t('Risk')}</TableHead>
          <TableHead>{t('Verdict')}</TableHead>
          <TableHead>{t('Scores')}</TableHead>
          <TableHead>{t('Core signals')}</TableHead>
          <TableHead>{t('Recommendation')}</TableHead>
          <TableHead>{t('Last seen')}</TableHead>
          <TableHead>{t('Status')}</TableHead>
          <TableHead className='text-right'>{t('Actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.items.map((item) => {
          const risk = getRiskLevelConfig(item.risk_level)
          const signals = parseRiskSignals(item.signals)
          return (
            <TableRow key={item.id}>
              <TableCell>
                <div className='flex flex-col gap-0.5'>
                  <span className='font-medium'>
                    {item.username || `#${item.user_id}`}
                  </span>
                  <span className='text-muted-foreground text-xs'>
                    {item.token_id > 0
                      ? `#${item.token_id} ${item.token_name || t('Unnamed token')}`
                      : t('All user tokens')}
                  </span>
                </div>
              </TableCell>
              <TableCell>
                <div className='flex items-center gap-2'>
                  <Badge variant='outline' className={risk.badgeClass}>
                    {t(risk.labelKey)}
                  </Badge>
                  <span className='font-mono text-sm font-semibold'>
                    {item.final_score}
                  </span>
                </div>
              </TableCell>
              <TableCell className='text-sm'>
                {t(verdictLabel(item.verdict))}
              </TableCell>
              <TableCell>
                <div className='text-xs tabular-nums'>
                  R {item.rule_score} · A {item.agent_score || '-'} · J{' '}
                  {item.judge_score || '-'}
                </div>
              </TableCell>
              <TableCell>
                {signals ? (
                  <div className='text-muted-foreground text-xs tabular-nums'>
                    RPM {signals.max_rpm} · {t('Concurrency')}{' '}
                    {signals.max_concurrency} · IP {signals.distinct_ips} · UA{' '}
                    {signals.distinct_uas}
                    {signals.distinct_tokens > 1 && (
                      <>
                        {' '}
                        · {t('Tokens')} {signals.distinct_tokens}
                      </>
                    )}
                  </div>
                ) : (
                  '-'
                )}
              </TableCell>
              <TableCell>
                <div className='flex flex-col gap-0.5 text-xs'>
                  <span>{t(actionLabel(item.recommended_action))}</span>
                  {item.recommended_duration_minutes > 0 && (
                    <span className='text-muted-foreground'>
                      {item.recommended_duration_minutes} {t('minutes')}
                    </span>
                  )}
                </div>
              </TableCell>
              <TableCell className='text-xs whitespace-nowrap'>
                {formatScreeningTimestamp(item.last_seen_at)}
              </TableCell>
              <TableCell>
                <Badge variant='secondary'>{t(statusLabel(item.status))}</Badge>
              </TableCell>
              <TableCell className='text-right'>
                <Button
                  size='icon-xs'
                  variant='ghost'
                  onClick={() => props.onOpen(item.id)}
                  aria-label={t('View details')}
                >
                  <Eye className='size-4' />
                </Button>
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}

function FilterSelect(props: {
  value: string
  placeholder: string
  options: string[]
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  return (
    <Select
      value={props.value}
      onValueChange={(value) => {
        if (value) props.onChange(value)
      }}
    >
      <SelectTrigger className='w-40'>
        <SelectValue placeholder={props.placeholder} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value='all'>{t('Any')}</SelectItem>
        {props.options.map((option) => (
          <SelectItem key={option} value={option}>
            {t(option.replaceAll('_', ' '))}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
