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
import { Bot, Loader2, ShieldCheck, Sparkles } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
  DrawerDescription,
} from '@/components/ui/drawer'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  analyzeRiskCase,
  applyRiskCaseAction,
  getRiskCaseDetail,
  reviewRiskCase,
} from '../api'
import { actionLabel, statusLabel, verdictLabel } from '../lib/risk-control'
import { formatScreeningTimestamp } from '../lib/utils'
import type {
  RiskAgentEvidence,
  RiskActionRequest,
  RiskActionType,
  RiskCaseDetail,
  RiskSuggestedFingerprint,
} from '../types'

interface RiskCaseDetailDrawerProps {
  caseId: number | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

const DEFAULT_ACTION: RiskActionRequest = {
  action: 'manual_review',
  duration_minutes: 360,
  request_limit: 10,
  reason: '',
  user_message: '',
}

export function RiskCaseDetailDrawer(props: RiskCaseDetailDrawerProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const role = useAuthStore((state) => state.auth.user?.role ?? ROLE.GUEST)
  const [actionForm, setActionForm] =
    useState<RiskActionRequest>(DEFAULT_ACTION)
  const [reviewNote, setReviewNote] = useState('')
  const [confirmActionOpen, setConfirmActionOpen] = useState(false)

  const detailQuery = useQuery({
    queryKey: ['risk-control', 'case', props.caseId],
    enabled: props.open && props.caseId !== null,
    queryFn: async () => {
      const result = await getRiskCaseDetail(props.caseId ?? 0)
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load risk case'))
      }
      return result.data
    },
  })

  useEffect(() => {
    const riskCase = detailQuery.data?.case
    if (!riskCase) return
    setActionForm({
      ...DEFAULT_ACTION,
      action: riskCase.recommended_action || 'manual_review',
      duration_minutes:
        riskCase.recommended_duration_minutes ||
        DEFAULT_ACTION.duration_minutes,
      reason: riskCase.recommended_reason || riskCase.rule_reason,
      user_message: riskCase.recommended_user_reason || '',
    })
    setReviewNote(riskCase.review_note || '')
  }, [detailQuery.data?.case])

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['risk-control', 'cases'] })
    void queryClient.invalidateQueries({
      queryKey: ['risk-control', 'case', props.caseId],
    })
  }

  const analyzeMutation = useMutation({
    mutationFn: () => analyzeRiskCase(props.caseId ?? 0),
    onSuccess: (result) => {
      if (!result.success) {
        toast.error(result.message || t('Agent analysis failed'))
        return
      }
      toast.success(t('Agent analysis completed'))
      invalidate()
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const reviewMutation = useMutation({
    mutationFn: (status: string) =>
      reviewRiskCase(props.caseId ?? 0, status, reviewNote),
    onSuccess: (result) => {
      if (!result.success) {
        toast.error(result.message || t('Failed to update review status'))
        return
      }
      toast.success(t('Review status updated'))
      invalidate()
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const actionMutation = useMutation({
    mutationFn: () => applyRiskCaseAction(props.caseId ?? 0, actionForm),
    onSuccess: (result) => {
      if (!result.success) {
        toast.error(result.message || t('Failed to apply risk action'))
        return
      }
      toast.success(t('Risk action applied'))
      invalidate()
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const detail = detailQuery.data
  const riskCase = detail?.case
  const requestAction = () => {
    if (
      ['freeze_token', 'temporary_block', 'permanent_ban', 'clear'].includes(
        actionForm.action
      )
    ) {
      setConfirmActionOpen(true)
      return
    }
    actionMutation.mutate()
  }
  const allowedActions: RiskActionType[] = ['none', 'observe', 'rate_limit']
  if ((detailQuery.data?.case.token_id ?? 0) > 0) {
    allowedActions.push('freeze_token')
  }
  allowedActions.push('temporary_block', 'manual_review', 'clear')
  if (role >= ROLE.SUPER_ADMIN) allowedActions.push('permanent_ban')

  return (
    <Drawer
      open={props.open}
      onOpenChange={props.onOpenChange}
      direction='right'
    >
      <DrawerContent className='w-full sm:max-w-3xl'>
        <DrawerHeader className='border-b'>
          <DrawerTitle>
            {riskCase ? `${t('Risk Case')} #${riskCase.id}` : t('Risk Case')}
          </DrawerTitle>
          <DrawerDescription>
            {riskCase
              ? `${riskCase.username || `#${riskCase.user_id}`} · ${t(
                  verdictLabel(riskCase.verdict)
                )}`
              : ''}
          </DrawerDescription>
        </DrawerHeader>

        <div className='min-h-0 flex-1 overflow-auto p-4'>
          {detailQuery.isLoading && (
            <div className='flex min-h-60 items-center justify-center'>
              <Loader2 className='text-muted-foreground size-6 animate-spin' />
            </div>
          )}
          {!detailQuery.isLoading &&
            (detailQuery.error || !detail || !riskCase) && (
              <p className='text-destructive text-sm'>
                {detailQuery.error?.message || t('Failed to load risk case')}
              </p>
            )}
          {!detailQuery.isLoading &&
            !detailQuery.error &&
            detail &&
            riskCase && (
              <div className='flex flex-col gap-5'>
                <CaseSummary detail={detail} />
                <DecisionCard
                  title={`${t('Rule engine')} · ${t(
                    verdictLabel(riskCase.rule_verdict || riskCase.verdict)
                  )}`}
                  icon={<ShieldCheck className='size-4' />}
                  score={riskCase.rule_score}
                  reason={riskCase.rule_reason}
                />
                {detail.agent_result && (
                  <DecisionCard
                    title={`${t('Triage Agent')} · ${t(
                      verdictLabel(detail.agent_result.verdict)
                    )}`}
                    icon={<Bot className='size-4' />}
                    score={detail.agent_result.risk_score}
                    reason={detail.agent_result.admin_reason}
                    evidence={detail.agent_result.evidence}
                    counterEvidence={detail.agent_result.counter_evidence}
                    meta={
                      riskCase.agent_model
                        ? `${riskCase.agent_model} · ${formatScreeningTimestamp(
                            riskCase.agent_analyzed_at
                          )}`
                        : undefined
                    }
                    policyViolation={detail.agent_result.policy_violation}
                    suggestedFingerprint={
                      detail.agent_result.suggested_fingerprint
                    }
                  />
                )}
                {detail.judge_result && (
                  <DecisionCard
                    title={`${t('Judge Agent')} · ${t(
                      verdictLabel(detail.judge_result.verdict)
                    )}`}
                    icon={<Sparkles className='size-4' />}
                    score={detail.judge_result.risk_score}
                    reason={detail.judge_result.admin_reason}
                    evidence={detail.judge_result.evidence}
                    counterEvidence={detail.judge_result.counter_evidence}
                    meta={
                      riskCase.judge_model
                        ? `${riskCase.judge_model} · ${formatScreeningTimestamp(
                            riskCase.judge_analyzed_at
                          )}`
                        : undefined
                    }
                    policyViolation={detail.judge_result.policy_violation}
                    suggestedFingerprint={
                      detail.judge_result.suggested_fingerprint
                    }
                  />
                )}

                <section className='rounded-xl border p-4'>
                  <div className='mb-3 flex items-center justify-between gap-2'>
                    <div>
                      <h3 className='font-semibold'>{t('Agent analysis')}</h3>
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          'Uses the configured direct channel without charging the user or writing user-visible content.'
                        )}
                      </p>
                    </div>
                    <Button
                      size='sm'
                      variant='outline'
                      disabled={analyzeMutation.isPending}
                      onClick={() => analyzeMutation.mutate()}
                    >
                      {analyzeMutation.isPending ? (
                        <Loader2 className='size-4 animate-spin' />
                      ) : (
                        <Bot className='size-4' />
                      )}
                      {t('Analyze now')}
                    </Button>
                  </div>
                </section>

                <ActionPanel
                  actionForm={actionForm}
                  setActionForm={setActionForm}
                  allowedActions={allowedActions}
                  pending={actionMutation.isPending}
                  onApply={requestAction}
                />

                <section className='rounded-xl border p-4'>
                  <h3 className='mb-3 font-semibold'>{t('Manual review')}</h3>
                  <Textarea
                    value={reviewNote}
                    onChange={(event) => setReviewNote(event.target.value)}
                    placeholder={t('Review note')}
                    className='min-h-24'
                  />
                  <div className='mt-3 flex flex-wrap gap-2'>
                    {['reviewing', 'resolved', 'dismissed', 'open'].map(
                      (status) => (
                        <Button
                          key={status}
                          size='sm'
                          variant='outline'
                          disabled={reviewMutation.isPending}
                          onClick={() => reviewMutation.mutate(status)}
                        >
                          {t(statusLabel(status))}
                        </Button>
                      )
                    )}
                  </div>
                </section>

                <EvidenceSamples detail={detail} />

                {detail.actions.length > 0 && (
                  <section className='rounded-xl border p-4'>
                    <h3 className='mb-3 font-semibold'>
                      {t('Action history')}
                    </h3>
                    <div className='flex flex-col gap-2'>
                      {detail.actions.map((action) => (
                        <div
                          key={action.id}
                          className='bg-muted/40 flex flex-col gap-1 rounded-lg p-3 text-xs'
                        >
                          <div className='flex items-center justify-between gap-2'>
                            <span className='font-medium'>
                              {t(actionLabel(action.action))}
                            </span>
                            <Badge variant='secondary'>{action.status}</Badge>
                          </div>
                          <span className='text-muted-foreground'>
                            {formatScreeningTimestamp(action.created_at)} ·{' '}
                            {action.source}
                          </span>
                          {action.reason && <p>{action.reason}</p>}
                        </div>
                      ))}
                    </div>
                  </section>
                )}
              </div>
            )}
        </div>

        <DrawerFooter className='border-t'>
          <DrawerClose asChild>
            <Button variant='outline'>{t('Close')}</Button>
          </DrawerClose>
        </DrawerFooter>
        <ConfirmDialog
          destructive
          open={confirmActionOpen}
          onOpenChange={setConfirmActionOpen}
          title={t('Confirm risk action?')}
          desc={t(
            'This action can disable access immediately. Verify the evidence and target before continuing.'
          )}
          confirmText={t('Apply action')}
          isLoading={actionMutation.isPending}
          handleConfirm={() => {
            setConfirmActionOpen(false)
            actionMutation.mutate()
          }}
        />
      </DrawerContent>
    </Drawer>
  )
}

function CaseSummary(props: { detail: RiskCaseDetail }) {
  const { t } = useTranslation()
  const riskCase = props.detail.case
  const signals = props.detail.signals
  return (
    <section className='bg-muted/30 rounded-xl border p-4'>
      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
        <Metric label={t('Final score')} value={String(riskCase.final_score)} />
        <Metric
          label={t('Confidence')}
          value={`${Math.round(riskCase.confidence * 100)}%`}
        />
        <Metric
          label={t('Repeat count')}
          value={String(riskCase.repeat_count)}
        />
        <Metric label={t('Window')} value={`${riskCase.window_hours}h`} />
        <Metric label={t('Max RPM')} value={String(signals?.max_rpm ?? '-')} />
        <Metric
          label={t('Max concurrency')}
          value={String(signals?.max_concurrency ?? '-')}
        />
        <Metric
          label={t('Distinct IPs')}
          value={String(signals?.distinct_ips ?? '-')}
        />
        <Metric
          label={t('Distinct UAs')}
          value={String(signals?.distinct_uas ?? '-')}
        />
        <Metric
          label={t('Distinct tokens')}
          value={String(signals?.distinct_tokens ?? '-')}
        />
        <Metric
          label={t('Total Quota')}
          value={String(signals?.total_quota ?? '-')}
        />
      </div>
    </section>
  )
}

function Metric(props: { label: string; value: string }) {
  return (
    <div className='flex flex-col gap-0.5'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className='font-mono text-lg font-semibold'>{props.value}</span>
    </div>
  )
}

function DecisionCard(props: {
  title: string
  icon: React.ReactNode
  score: number
  reason: string
  evidence?: RiskAgentEvidence[]
  counterEvidence?: string[]
  meta?: string
  policyViolation?: boolean
  suggestedFingerprint?: RiskSuggestedFingerprint
}) {
  const { t } = useTranslation()
  return (
    <section className='rounded-xl border p-4'>
      <div className='mb-2 flex items-center gap-2'>
        {props.icon}
        <h3 className='font-semibold'>{props.title}</h3>
        <Badge variant='secondary' className='ml-auto'>
          {props.score}/100
        </Badge>
      </div>
      {(props.meta || props.policyViolation) && (
        <div className='text-muted-foreground mb-2 flex flex-wrap items-center gap-2 text-xs'>
          {props.meta && <span>{props.meta}</span>}
          {props.policyViolation && (
            <Badge variant='destructive'>{t('Policy violation')}</Badge>
          )}
        </div>
      )}
      {props.reason && (
        <p className='text-sm whitespace-pre-wrap'>{props.reason}</p>
      )}
      {props.evidence && props.evidence.length > 0 && (
        <div className='mt-3'>
          <p className='text-xs font-medium'>{t('Evidence')}</p>
          <ul className='text-muted-foreground mt-1 list-disc space-y-1 pl-5 text-xs'>
            {props.evidence.map((item) => (
              <li
                key={`${item.signal_id}-${item.strength}-${item.summary}-${item.request_ids.join('|')}`}
              >
                <span className='font-medium'>[{item.signal_id}]</span>{' '}
                {item.summary}
                {item.request_ids.length > 0 && (
                  <span className='text-muted-foreground'>
                    {' '}
                    · {t('Requests')}: {item.request_ids.join(', ')}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
      {props.counterEvidence && props.counterEvidence.length > 0 && (
        <div className='mt-3'>
          <p className='text-xs font-medium'>{t('Counter evidence')}</p>
          <ul className='text-muted-foreground mt-1 list-disc space-y-1 pl-5 text-xs'>
            {props.counterEvidence.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </div>
      )}
      {props.suggestedFingerprint &&
        props.suggestedFingerprint.kind &&
        props.suggestedFingerprint.kind !== 'none' && (
          <div className='bg-muted/40 mt-3 rounded-lg p-3 text-xs'>
            <p className='font-medium'>
              {t('Suggested fingerprint')} · {props.suggestedFingerprint.kind}
            </p>
            {props.suggestedFingerprint.pattern && (
              <code className='mt-1 block break-all'>
                {props.suggestedFingerprint.pattern}
              </code>
            )}
            {props.suggestedFingerprint.reason && (
              <p className='text-muted-foreground mt-1'>
                {props.suggestedFingerprint.reason}
              </p>
            )}
          </div>
        )}
    </section>
  )
}

function ActionPanel(props: {
  actionForm: RiskActionRequest
  setActionForm: (value: RiskActionRequest) => void
  allowedActions: RiskActionType[]
  pending: boolean
  onApply: () => void
}) {
  const { t } = useTranslation()
  const needsDuration = ['rate_limit', 'temporary_block', 'observe'].includes(
    props.actionForm.action
  )
  return (
    <section className='rounded-xl border border-orange-500/30 bg-orange-500/5 p-4'>
      <h3 className='mb-1 font-semibold'>{t('Apply risk action')}</h3>
      <p className='text-muted-foreground mb-4 text-xs'>
        {t(
          'Permanent bans are root-only. Temporary restrictions are enforced from the existing user cache.'
        )}{' '}
        {t(
          'Clearing a restriction does not re-enable a frozen token or banned account.'
        )}
      </p>
      <div className='grid gap-3 sm:grid-cols-2'>
        <div className='space-y-1'>
          <Label>{t('Action')}</Label>
          <Select
            value={props.actionForm.action}
            onValueChange={(value) =>
              props.setActionForm({
                ...props.actionForm,
                action: value as RiskActionType,
              })
            }
          >
            <SelectTrigger className='w-full'>
              <SelectValue>
                {t(actionLabel(props.actionForm.action))}
              </SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              {props.allowedActions.map((action) => (
                <SelectItem key={action} value={action}>
                  {t(actionLabel(action))}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {needsDuration && (
          <div className='space-y-1'>
            <Label>{t('Duration minutes')}</Label>
            <Input
              type='number'
              min={1}
              value={props.actionForm.duration_minutes}
              onChange={(event) =>
                props.setActionForm({
                  ...props.actionForm,
                  duration_minutes: Number(event.target.value),
                })
              }
            />
          </div>
        )}
        {props.actionForm.action === 'rate_limit' && (
          <div className='space-y-1'>
            <Label>{t('Requests per minute')}</Label>
            <Input
              type='number'
              min={1}
              value={props.actionForm.request_limit}
              onChange={(event) =>
                props.setActionForm({
                  ...props.actionForm,
                  request_limit: Number(event.target.value),
                })
              }
            />
          </div>
        )}
      </div>
      <div className='mt-3 grid gap-3 sm:grid-cols-2'>
        <div className='space-y-1'>
          <Label>{t('Admin reason')}</Label>
          <Textarea
            value={props.actionForm.reason}
            onChange={(event) =>
              props.setActionForm({
                ...props.actionForm,
                reason: event.target.value,
              })
            }
          />
        </div>
        <div className='space-y-1'>
          <Label>{t('User-facing reason')}</Label>
          <Textarea
            value={props.actionForm.user_message}
            onChange={(event) =>
              props.setActionForm({
                ...props.actionForm,
                user_message: event.target.value,
              })
            }
          />
        </div>
      </div>
      <div className='mt-3 flex justify-end'>
        <Button disabled={props.pending} onClick={props.onApply}>
          {props.pending && <Loader2 className='size-4 animate-spin' />}
          {t('Apply action')}
        </Button>
      </div>
    </section>
  )
}

function EvidenceSamples(props: { detail: RiskCaseDetail }) {
  const { t } = useTranslation()
  const samples = props.detail.signals?.samples ?? []
  if (samples.length === 0) return null
  return (
    <section className='rounded-xl border p-4'>
      <h3 className='mb-3 font-semibold'>{t('Evidence samples')}</h3>
      <div className='flex flex-col gap-3'>
        {samples.map((sample) => (
          <div
            key={`${sample.request_id}-${sample.created_at}-${sample.ip}-${sample.user_agent}-${sample.model}-${sample.request_path}`}
            className='bg-muted/40 rounded-lg p-3'
          >
            <div className='text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-xs'>
              <span>{formatScreeningTimestamp(sample.created_at)}</span>
              <span>{sample.ip || '-'}</span>
              <span>{sample.model || '-'}</span>
              <span>{sample.request_path || '-'}</span>
            </div>
            {sample.user_agent && (
              <p className='mt-2 font-mono text-xs break-all'>
                {sample.user_agent}
              </p>
            )}
            {sample.request_params && (
              <pre className='bg-background mt-2 max-h-48 overflow-auto rounded border p-2 text-xs break-all whitespace-pre-wrap'>
                {sample.request_params}
              </pre>
            )}
          </div>
        ))}
      </div>
    </section>
  )
}
