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
 * Aggregated error signatures table with per-row delete.
 */
import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Eye, Loader2, Settings, Sparkles, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
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
import { formatCompactNumber } from '@/lib/format'
import { formatTimestamp } from '@/lib/format'
import {
  deleteErrorInsightSignature,
  generateErrorInsightAIRules,
  getErrorInsightLogs,
  getErrorInsightSignatures,
  saveErrorInsightAIRule,
} from '../api'
import type {
  ErrorInsightAIGenerateResult,
  ErrorInsightFilterParams,
  ErrorInsightLog,
  ErrorInsightAIRuleSuggestion,
} from '../types'
import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

interface SignaturesTableProps {
  params: ErrorInsightFilterParams
  onOpenAISettings?: () => void
}

export function SignaturesTable(props: SignaturesTableProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)
  const [sampleSignature, setSampleSignature] = useState<string | null>(null)
  const [aiPanelSignature, setAIPanelSignature] = useState<string | null>(null)
  const [aiResults, setAIResults] = useState<Record<string, ErrorInsightAIGenerateResult>>({})
  const [editableRulesBySignature, setEditableRulesBySignature] = useState<Record<string, ErrorInsightAIRuleSuggestion[]>>({})
  const [generatingSignatures, setGeneratingSignatures] = useState<Record<string, boolean>>({})

  const queryKey = useMemo(
    () => ['error-insight', 'signatures', props.params] as const,
    [props.params]
  )

  const {
    data: signatures,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey,
    queryFn: async () => {
      const result = await getErrorInsightSignatures(props.params)
      if (!result.success) {
        throw new Error(
          result.message || t('Failed to load signatures')
        )
      }
      return result.data ?? []
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (signature: string) =>
      deleteErrorInsightSignature(signature),
    onSuccess: (result) => {
      if (!result.success) {
        toast.error(result.message || t('Failed to delete signature'))
        return
      }
      toast.success(
        t('Deleted {{count}} log(s) for this signature', {
          count: result.data?.deleted ?? 0,
        })
      )
      setPendingDelete(null)
      void queryClient.invalidateQueries({
        queryKey: ['error-insight'],
      })
    },
    onError: () => {
      toast.error(t('Failed to delete signature'))
    },
  })

  const handleGenerateAI = async (signature: string) => {
    if (generatingSignatures[signature]) return
    setGeneratingSignatures((current) => ({ ...current, [signature]: true }))
    try {
      const result = await generateErrorInsightAIRules(signature)
      if (!result.success || !result.data) {
        toast.error(result.message || t('Failed to generate rules'))
        return
      }
      setAIResults((current) => ({ ...current, [signature]: result.data }))
      setEditableRulesBySignature((current) => ({
        ...current,
        [signature]: result.data.rules,
      }))
      if (result.data.rules.length > 0) {
        toast.success(t('Candidate rules generated. Click the AI result eye to review.'))
      } else {
        toast.warning(t('AI did not return usable candidate rules.'))
      }
    } catch (error) {
      toast.error(error.message || t('Failed to generate rules'))
    } finally {
      setGeneratingSignatures((current) => {
        const next = { ...current }
        delete next[signature]
        return next
      })
    }
  }

  const handleConfirmDelete = () => {
    if (!pendingDelete) return
    deleteMutation.mutate(pendingDelete)
  }

  const saveRuleMutation = useMutation({
    mutationFn: (rule: ErrorInsightAIRuleSuggestion) => saveErrorInsightAIRule(rule),
    onSuccess: (result) => {
      if (!result.success) {
        toast.error(result.message || t('Failed to save candidate rule'))
        return
      }
      toast.success(t('Candidate rule saved'))
      void queryClient.invalidateQueries({ queryKey: ['error-insight'] })
      void queryClient.invalidateQueries({ queryKey: ['system-options'] })
    },
    onError: (error) => {
      toast.error(error.message || t('Failed to save candidate rule'))
    },
  })

  const updateEditableRule = (
    index: number,
    patch: Partial<ErrorInsightAIRuleSuggestion>
  ) => {
    if (!aiPanelSignature) return
    setEditableRulesBySignature((current) => ({
      ...current,
      [aiPanelSignature]: (current[aiPanelSignature] ?? []).map((rule, ruleIndex) =>
        ruleIndex === index ? { ...rule, ...patch } : rule
      ),
    }))
  }

  const items = signatures ?? []
  const selectedAIResult = aiPanelSignature ? aiResults[aiPanelSignature] : null
  const editableRules = aiPanelSignature ? editableRulesBySignature[aiPanelSignature] ?? [] : []

  const sampleQuery = useQuery({
    queryKey: [
      'error-insight',
      'sample-logs',
      { ...props.params, normalized_signature: sampleSignature, page_size: 10 },
    ],
    enabled: Boolean(sampleSignature),
    queryFn: async () => {
      const result = await getErrorInsightLogs({
        ...props.params,
        normalized_signature: sampleSignature || undefined,
        page: 1,
        page_size: 10,
      })
      if (!result.success) {
        throw new Error(result.message || t('Failed to load sample logs'))
      }
      return result.data
    },
  })

  return (
    <div className='bg-card flex flex-col overflow-hidden rounded-2xl border-0 shadow-sm'>
      <div className='flex flex-wrap items-start justify-between gap-3 px-6 py-5'>
        <div className='space-y-1'>
          <h2 className='text-xl font-bold'>
            {t('Top Unmatched Error Signatures')}
          </h2>
          <p className='text-primary/70 text-sm'>
            {t(
              'Sorted by occurrences, affected users, and latest seen time. Click to view sample logs.'
            )}
          </p>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            variant='outline'
            size='sm'
            onClick={props.onOpenAISettings}
          >
            <Settings className='size-4' />
            {t('AI Settings')}
          </Button>
          <Badge className='bg-amber-500/20 text-amber-500 hover:bg-amber-500/20'>
            {t('Default unmatched only')}
          </Badge>
        </div>
      </div>
      <div className='overflow-x-auto'>
        {error ? (
          <ErrorState
            title={t('Failed to load')}
            description={error.message}
            onRetry={refetch}
            className='min-h-[240px]'
          />
        ) : isLoading && items.length === 0 ? (
          <div className='flex min-h-[240px] flex-col items-center justify-center gap-3'>
            <Loader2 className='text-muted-foreground size-6 animate-spin' />
            <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
          </div>
        ) : items.length === 0 ? (
          <EmptyState
            title={t('No signatures found')}
            description={t('No error signatures match the current filters.')}
            className='min-h-[240px]'
          />
        ) : (
          <Table>
            <TableHeader className='bg-muted/60 sticky top-0 z-10'>
              <TableRow>
                <TableHead className='min-w-[300px] px-6 py-4 text-sm font-semibold'>
                  {t('Signature')}
                </TableHead>
                <TableHead className='w-[190px] py-4 text-sm font-semibold'>
                  {t('Current Category')}
                </TableHead>
                <TableHead className='w-[260px] py-4 text-sm font-semibold'>
                  {t('Reason')}
                </TableHead>
                <TableHead className='w-[110px] py-4 text-right text-sm font-semibold'>
                  {t('Count')}
                </TableHead>
                <TableHead className='w-[150px] py-4 text-center text-sm font-semibold'>
                  {t('Impact')}
                </TableHead>
                <TableHead className='w-[190px] py-4 text-sm font-semibold'>
                  {t('Latest Seen')}
                </TableHead>
                <TableHead className='w-[100px] py-4 text-right text-sm font-semibold'>
                  {t('Actions')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((signature) => {
                const isMatched = Boolean(signature.rule_code)
                const aiResult = aiResults[signature.normalized_signature]
                const hasAIResult = Boolean(aiResult)
                const isGeneratingAI = Boolean(generatingSignatures[signature.normalized_signature])
                return (
                  <TableRow
                    key={signature.normalized_signature}
                    className='border-dashed'
                  >
                    <TableCell className='px-6 py-5'>
                      <div className='flex flex-col gap-1'>
                        <span className='max-w-[280px] truncate font-mono text-sm'>
                          {signature.normalized_signature}
                        </span>
                        <div className='flex items-center gap-2'>
                          <span className='text-primary/70 max-w-[260px] truncate text-sm'>
                            {signature.error_source || '-'}
                            {signature.error_stage
                              ? `/${signature.error_stage}`
                              : ''}
                          </span>
                          <CopyButton
                            value={signature.normalized_signature}
                            size='icon'
                            variant='ghost'
                            tooltip={t('Copy signature')}
                          />
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className='py-5'>
                      {isMatched ? (
                        <Badge variant='secondary' className='font-semibold'>
                          {signature.rule_code}
                        </Badge>
                      ) : (
                        <span className='font-semibold'>
                          {signature.normalized_message || '-'}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className='py-5'>
                      {signature.unmatched_reason ? (
                        <span className='font-semibold'>
                          {signature.unmatched_reason}
                        </span>
                      ) : (
                        <span className='text-muted-foreground'>
                          -
                        </span>
                      )}
                    </TableCell>
                    <TableCell className='py-5 text-right text-lg font-bold tabular-nums'>
                      {formatCompactNumber(signature.count)}
                    </TableCell>
                    <TableCell className='py-5 text-center'>
                      <div className='flex flex-col gap-0.5 font-semibold'>
                        <span>
                          {t('User')}{' '}
                          {formatCompactNumber(signature.affected_users)}
                        </span>
                        <span className='text-primary/70 text-sm font-medium'>
                          {t('Channel')}{' '}
                          {formatCompactNumber(signature.affected_channels)}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className='py-5 text-base font-semibold whitespace-nowrap'>
                      {formatTimestamp(signature.latest_at)}
                    </TableCell>
                    <TableCell className='py-5 text-right'>
                      <Button
                        variant='ghost'
                        size='icon'
                        className='text-muted-foreground hover:text-foreground size-8'
                        aria-label={t('View Details')}
                        onClick={() =>
                          setSampleSignature(signature.normalized_signature)
                        }
                      >
                        <Eye className='size-4' />
                      </Button>
                      <Button
                        variant='ghost'
                        size='icon'
                        className='text-muted-foreground hover:text-amber-500 size-8'
                        aria-label={t('AI Generate Rules')}
                        disabled={isGeneratingAI}
                        onClick={() => handleGenerateAI(signature.normalized_signature)}
                      >
                        {isGeneratingAI ? (
                          <Loader2 className='size-4 animate-spin' />
                        ) : (
                          <Sparkles className='size-4' />
                        )}
                      </Button>
                      <Button
                        variant='ghost'
                        size='icon'
                        className={
                          hasAIResult
                            ? 'text-cyan-500 hover:text-cyan-400 size-8'
                            : 'text-muted-foreground/40 size-8'
                        }
                        aria-label={t('View AI Result')}
                        disabled={!hasAIResult}
                        onClick={() =>
                          setAIPanelSignature(signature.normalized_signature)
                        }
                      >
                        <Eye className='size-4' />
                      </Button>
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <Button
                              variant='ghost'
                              size='icon'
                              className='text-muted-foreground hover:text-destructive size-7'
                              aria-label={t('Delete signature')}
                              onClick={() =>
                                setPendingDelete(
                                  signature.normalized_signature
                                )
                              }
                            />
                          }
                        >
                          <Trash2 className='size-4' />
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>{t('Delete signature')}</p>
                        </TooltipContent>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </div>

      <ConfirmDialog
        destructive
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
        handleConfirm={handleConfirmDelete}
        isLoading={deleteMutation.isPending}
        className='max-w-md'
        title={t('Delete Signature?')}
        desc={
          <>
            {t(
              'This will permanently delete all error logs grouped under this signature.'
            )}
            <br />
            {t('This action cannot be undone.')}
            {pendingDelete && (
              <code className='bg-muted mt-2 block max-w-full truncate rounded px-1.5 py-0.5 font-mono text-xs'>
                {pendingDelete}
              </code>
            )}
          </>
        }
        confirmText={t('Delete')}
      />

      <Dialog
        open={sampleSignature !== null}
        onOpenChange={(open) => {
          if (!open) setSampleSignature(null)
        }}
      >
        <DialogContent className='max-h-[88vh] overflow-y-auto p-6 sm:max-w-6xl'>
          <DialogHeader className='gap-3'>
            <DialogTitle className='text-2xl font-bold'>
              {t('Error Signature Sample Logs')}
            </DialogTitle>
            <DialogDescription className='break-all font-mono text-xs'>
              {sampleSignature || '-'}
            </DialogDescription>
          </DialogHeader>

          <div className='space-y-4'>
            {sampleQuery.error ? (
              <ErrorState
                title={t('Failed to load sample logs')}
                description={sampleQuery.error.message}
                onRetry={sampleQuery.refetch}
                className='min-h-[240px]'
              />
            ) : sampleQuery.isLoading ? (
              <div className='flex min-h-[240px] flex-col items-center justify-center gap-3'>
                <Loader2 className='text-muted-foreground size-6 animate-spin' />
                <p className='text-muted-foreground text-sm'>
                  {t('Loading...')}
                </p>
              </div>
            ) : sampleQuery.data?.logs.length ? (
              sampleQuery.data.logs.map((log) => (
                <SampleLogCard key={log.id} log={log} />
              ))
            ) : (
              <EmptyState
                title={t('No logs found')}
                description={t('No error logs match the current filters.')}
                className='min-h-[240px]'
              />
            )}
          </div>
        </DialogContent>
      </Dialog>

      <Sheet
        open={aiPanelSignature !== null}
        onOpenChange={(open) => {
          if (!open) setAIPanelSignature(null)
        }}
      >
        <SheetContent className='w-[92vw] p-0 sm:max-w-3xl' side='right'>
          <SheetHeader className='border-border/70 border-b px-6 py-5'>
            <SheetTitle className='text-2xl font-bold'>
              {t('AI Candidate Rules')}
            </SheetTitle>
            <SheetDescription className='space-y-2'>
              <span className='block'>
                {t('Generated results are stored on this row. Review them from this side panel before saving.')}
              </span>
              <code className='bg-muted block max-w-full truncate rounded px-2 py-1 font-mono text-xs'>
                {aiPanelSignature || '-'}
              </code>
            </SheetDescription>
          </SheetHeader>

          <div className='flex-1 overflow-y-auto px-6 py-5'>
            {selectedAIResult?.raw ? (
              <details className='border-border/70 bg-muted/30 mb-4 rounded-xl border p-3'>
                <summary className='text-muted-foreground cursor-pointer text-sm font-semibold'>
                  {t('Raw AI Output')}
                </summary>
                <pre className='mt-3 max-h-48 overflow-auto whitespace-pre-wrap break-words text-xs'>
                  {JSON.stringify(selectedAIResult.raw, null, 2)}
                </pre>
              </details>
            ) : null}

            <div className='space-y-4'>
              {editableRules.length ? (
                editableRules.map((rule, index) => (
                  <div key={`${rule.rule_code}-${index}`} className='bg-muted/40 rounded-2xl p-5'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <Badge variant='secondary'>{t('Candidate')} #{index + 1}</Badge>
                      <Badge variant='outline'>{rule.category || '-'}</Badge>
                      <Badge className='bg-cyan-500/20 text-cyan-500 hover:bg-cyan-500/20'>
                        {rule.match_type || '-'}
                      </Badge>
                      <Badge className='bg-amber-500/20 text-amber-500 hover:bg-amber-500/20'>
                        {t('Confidence')} {Math.round((rule.confidence || 0) * 100)}%
                      </Badge>
                    </div>
                    <div className='mt-4 grid gap-4 md:grid-cols-2'>
                      <RuleInput
                        label={t('Rule Code')}
                        value={rule.rule_code}
                        onChange={(value) => updateEditableRule(index, { rule_code: value })}
                      />
                      <RuleSelect
                        label={t('Match Type')}
                        value={rule.match_type}
                        onChange={(value) => updateEditableRule(index, { match_type: value })}
                      />
                      <RuleTextarea
                        label={t('Match Pattern')}
                        value={rule.match_pattern}
                        onChange={(value) => updateEditableRule(index, { match_pattern: value })}
                      />
                      <RuleInput
                        label={t('Safe Error Code')}
                        value={rule.safe_error_code}
                        onChange={(value) => updateEditableRule(index, { safe_error_code: value })}
                      />
                      <RuleInput
                        label={t('Safe Error Type')}
                        value={rule.safe_error_type}
                        onChange={(value) => updateEditableRule(index, { safe_error_type: value })}
                      />
                      <RuleTextarea
                        label={t('Safe Error Message')}
                        value={rule.safe_error_message}
                        onChange={(value) => updateEditableRule(index, { safe_error_message: value })}
                      />
                    </div>
                    <div className='mt-4'>
                      <p className='text-muted-foreground text-xs font-semibold'>{t('Reason')}</p>
                      <p className='mt-1 text-sm whitespace-pre-wrap'>{rule.reason || '-'}</p>
                    </div>
                    <div className='mt-5 flex justify-end'>
                      <Button
                        disabled={saveRuleMutation.isPending}
                        onClick={() => saveRuleMutation.mutate(rule)}
                      >
                        {saveRuleMutation.isPending && <Loader2 className='size-4 animate-spin' />}
                        {t('Approve and Save Rule')}
                      </Button>
                    </div>
                  </div>
                ))
              ) : (
                <EmptyState
                  title={t('No candidate rules')}
                  description={t('AI did not return usable candidate rules.')}
                  className='min-h-[260px]'
                />
              )}
            </div>
          </div>
        </SheetContent>
      </Sheet>
    </div>
  )
}

function RuleInput(props: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <div className='space-y-1.5'>
      <p className='text-muted-foreground text-xs font-semibold'>{props.label}</p>
      <input
        value={props.value || ''}
        className='border-input bg-background ring-offset-background focus-visible:ring-ring flex h-9 w-full rounded-md border px-3 py-1 text-sm transition-colors focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none'
        onChange={(event) => props.onChange(event.target.value)}
      />
    </div>
  )
}

function RuleTextarea(props: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <div className='space-y-1.5'>
      <p className='text-muted-foreground text-xs font-semibold'>{props.label}</p>
      <textarea
        value={props.value || ''}
        className='border-input bg-background ring-offset-background focus-visible:ring-ring flex min-h-24 w-full rounded-md border px-3 py-2 text-sm transition-colors focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none'
        onChange={(event) => props.onChange(event.target.value)}
      />
    </div>
  )
}

function RuleSelect(props: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <div className='space-y-1.5'>
      <p className='text-muted-foreground text-xs font-semibold'>{props.label}</p>
      <select
        value={props.value || 'contains'}
        className='border-input bg-background ring-offset-background focus-visible:ring-ring flex h-9 w-full rounded-md border px-3 py-1 text-sm transition-colors focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none'
        onChange={(event) => props.onChange(event.target.value)}
      >
        <option value='contains'>contains</option>
        <option value='regex'>regex</option>
      </select>
    </div>
  )
}

function SampleLogCard({ log }: { log: ErrorInsightLog }) {
  const { t } = useTranslation()
  return (
    <div className='bg-muted/40 rounded-2xl p-5'>
      <div className='text-primary/60 flex flex-wrap gap-x-6 gap-y-2 text-sm font-medium'>
        <span>{formatTimestamp(log.created_at)}</span>
        <span>
          {t('User')}: {log.user_id || '-'}
        </span>
        <span>
          {t('Channel')}: {log.channel_id || '-'}
        </span>
        <span>
          {t('Model')}: {log.model_name || '-'}
        </span>
      </div>

      <div className='mt-3 flex flex-wrap gap-2'>
        <Badge variant='secondary'>
          {t('Client')} {log.client_status_code || '-'}
        </Badge>
        <Badge variant='secondary'>
          {t('Upstream')} {log.upstream_status_code || '-'}
        </Badge>
        <Badge className='bg-amber-500/20 text-amber-500 hover:bg-amber-500/20'>
          {log.rule_matched ? t('Matched') : t('Unmatched')}
        </Badge>
        <Badge variant='outline'>{log.rule_code || '-'}</Badge>
      </div>

      <p className='mt-3 font-mono text-sm break-words whitespace-pre-wrap'>
        {log.safe_error_message || '-'}
        {log.request_id ? ` (request id: ${log.request_id})` : ''}
      </p>

      <div className='border-border mt-4 space-y-3 border-t border-dashed pt-4'>
        <div className='flex flex-wrap items-center gap-2'>
          <Badge className='bg-orange-500/20 text-orange-500 hover:bg-orange-500/20'>
            {t('Original Error')}
          </Badge>
          <span className='text-primary/60 text-sm'>
            {t(
              'Visible to admin/root only. Secrets, tokens, and cookies are masked.'
            )}
          </span>
        </div>
        <pre className='overflow-x-auto rounded-lg bg-orange-100 p-4 font-mono text-sm whitespace-pre-wrap text-red-700 dark:bg-orange-100 dark:text-red-700'>
          {log.original_error_message || '-'}
        </pre>
        <div>
          <p className='text-cyan-500 text-sm font-semibold'>
            {t('Normalized Error Text')}:
          </p>
          <p className='mt-2 font-mono text-sm break-words whitespace-pre-wrap'>
            {log.normalized_signature || '-'}
          </p>
        </div>
      </div>
    </div>
  )
}
