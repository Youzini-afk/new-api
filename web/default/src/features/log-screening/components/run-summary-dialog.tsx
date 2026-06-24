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
 * Dialog summarizing a manual screening run, including the DoS cap / truncation
 * indicators (capped, candidate_limit, detail_limit, candidates_seen,
 * details_seen).
 */
import { useTranslation } from 'react-i18next'
import { TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { formatScreeningNumber, formatScreeningTimestamp } from '../lib/utils'
import type { LogScreeningRunSummary } from '../types'

interface RunSummaryDialogProps {
  summary: LogScreeningRunSummary | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function RunSummaryDialog(props: RunSummaryDialogProps) {
  const { t } = useTranslation()
  const { summary } = props

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Screening run summary')}</DialogTitle>
        </DialogHeader>
        {summary && (
          <div className='max-h-[60vh] overflow-auto'>
            <div className='mb-3 flex flex-wrap items-center gap-2'>
              <Badge variant={summary.enabled ? 'default' : 'secondary'}>
                {summary.enabled ? t('Enabled') : t('Disabled')}
              </Badge>
              <Badge variant='outline'>{summary.status}</Badge>
              <Badge variant='outline'>{summary.kind}</Badge>
              {summary.manual && <Badge variant='secondary'>{t('Manual')}</Badge>}
            </div>

            {!summary.enabled && (
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Log screening is disabled in settings. No records were written.'
                )}
              </p>
            )}

            {summary.capped && (
              <div className='mb-3 flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs'>
                <TriangleAlert className='mt-0.5 size-3.5 text-amber-600 dark:text-amber-400' />
                <div>
                  <p className='font-medium'>
                    {t('Run was truncated to bound memory and DB load.')}
                  </p>
                  <p className='text-muted-foreground mt-1'>
                    {t(
                      'Candidates seen {{seen}} of limit {{limit}}; details seen {{dSeen}} of limit {{dLimit}}.',
                      {
                        seen: formatScreeningNumber(summary.candidates_seen),
                        limit: formatScreeningNumber(summary.candidate_limit),
                        dSeen: formatScreeningNumber(summary.details_seen),
                        dLimit: formatScreeningNumber(summary.detail_limit),
                      }
                    )}
                  </p>
                </div>
              </div>
            )}

            <div className='grid grid-cols-2 gap-3 text-sm'>
              <Field label={t('Rules total')}>
                {formatScreeningNumber(summary.rules_total)}
              </Field>
              <Field label={t('Rules checked')}>
                {formatScreeningNumber(summary.rules_checked)}
              </Field>
              <Field label={t('Records created')}>
                {formatScreeningNumber(summary.records_created)}
              </Field>
              <Field label={t('Records updated')}>
                {formatScreeningNumber(summary.records_updated)}
              </Field>
              <Field label={t('Expired cleaned')}>
                {formatScreeningNumber(summary.expired)}
              </Field>
              <Field label={t('Elapsed (ms)')}>
                {formatScreeningNumber(summary.elapsed_ms)}
              </Field>
              <Field label={t('Window start')}>
                {formatScreeningTimestamp(summary.window_start)}
              </Field>
              <Field label={t('Window end')}>
                {formatScreeningTimestamp(summary.window_end)}
              </Field>
              <Field label={t('Started at')}>
                {formatScreeningTimestamp(summary.started_at)}
              </Field>
              <Field label={t('Finished at')}>
                {formatScreeningTimestamp(summary.finished_at)}
              </Field>
              <Field label={t('Operator')}>
                {summary.operator_name || '-'}
              </Field>
            </div>
          </div>
        )}
        <div className='flex justify-end'>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Close')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function Field(props: { label: string; children: React.ReactNode }) {
  return (
    <div className='flex flex-col gap-0.5'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className='break-words'>{props.children}</span>
    </div>
  )
}
