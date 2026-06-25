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
import { Loader2, Trash2 } from 'lucide-react'
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
import { deleteErrorInsightSignature, getErrorInsightSignatures } from '../api'
import type { ErrorInsightFilterParams } from '../types'
import { ConfirmDialog } from '@/components/confirm-dialog'

interface SignaturesTableProps {
  params: ErrorInsightFilterParams
}

export function SignaturesTable(props: SignaturesTableProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)

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

  const handleConfirmDelete = () => {
    if (!pendingDelete) return
    deleteMutation.mutate(pendingDelete)
  }

  const items = signatures ?? []

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
            <TableHeader className='bg-muted/50 sticky top-0 z-10'>
              <TableRow>
                <TableHead className='min-w-[260px]'>
                  {t('Signature')}
                </TableHead>
                <TableHead className='w-[140px]'>{t('Rule Code')}</TableHead>
                <TableHead className='w-[160px]'>
                  {t('Unmatched Reason')}
                </TableHead>
                <TableHead className='w-[110px] text-right'>
                  {t('Count')}
                </TableHead>
                <TableHead className='w-[120px] text-right'>
                  {t('Affected Users')}
                </TableHead>
                <TableHead className='w-[130px] text-right'>
                  {t('Affected Channels')}
                </TableHead>
                <TableHead className='w-[160px]'>{t('First Seen')}</TableHead>
                <TableHead className='w-[160px]'>{t('Latest At')}</TableHead>
                <TableHead className='w-[80px] text-right'>
                  {t('Actions')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((signature) => {
                const isMatched = Boolean(signature.rule_code)
                return (
                  <TableRow key={signature.normalized_signature}>
                    <TableCell>
                      <div className='flex flex-col gap-1'>
                        <span className='line-clamp-2 text-sm font-medium'>
                          {signature.normalized_message || '-'}
                        </span>
                        <div className='flex items-center gap-1.5'>
                          <code className='bg-muted text-muted-foreground max-w-[260px] truncate rounded px-1.5 py-0.5 font-mono text-xs'>
                            {signature.normalized_signature}
                          </code>
                          <CopyButton
                            value={signature.normalized_signature}
                            size='icon'
                            variant='ghost'
                            tooltip={t('Copy signature')}
                          />
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      {isMatched ? (
                        <Badge variant='secondary'>
                          {signature.rule_code}
                        </Badge>
                      ) : (
                        <span className='text-muted-foreground text-xs'>
                          {t('None')}
                        </span>
                      )}
                    </TableCell>
                    <TableCell>
                      {signature.unmatched_reason ? (
                        <span className='text-muted-foreground text-xs'>
                          {signature.unmatched_reason}
                        </span>
                      ) : (
                        <span className='text-muted-foreground text-xs'>
                          -
                        </span>
                      )}
                    </TableCell>
                    <TableCell className='text-right font-medium tabular-nums'>
                      {formatCompactNumber(signature.count)}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {formatCompactNumber(signature.affected_users)}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {formatCompactNumber(signature.affected_channels)}
                    </TableCell>
                    <TableCell className='text-muted-foreground text-xs'>
                      {formatTimestamp(signature.first_seen_at)}
                    </TableCell>
                    <TableCell className='text-muted-foreground text-xs'>
                      {formatTimestamp(signature.latest_at)}
                    </TableCell>
                    <TableCell className='text-right'>
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
    </div>
  )
}
