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
 * Dialog for appending an admin remark to a screening record or block log.
 * The backend appends the remark to the target user's remark field.
 */
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'

interface RemarkDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  /** Existing user remark, shown read-only for context. */
  currentRemark?: string
  /** Mutation that performs the POST; receives the trimmed remark. */
  submit: (remark: string) => Promise<{ success: boolean; message?: string }>
  /** Query keys to invalidate after a successful submit. */
  invalidateQueries?: unknown[][]
}

export function RemarkDialog(props: RemarkDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [value, setValue] = useState('')

  useEffect(() => {
    if (props.open) setValue('')
  }, [props.open])

  const mutation = useMutation({
    mutationFn: (remark: string) => props.submit(remark),
    onSuccess: async (data) => {
      if (data.success) {
        toast.success(t('Remark appended'))
        if (props.invalidateQueries) {
          for (const key of props.invalidateQueries) {
            await queryClient.invalidateQueries({ queryKey: key })
          }
        }
        props.onOpenChange(false)
      } else {
        toast.error(data.message || t('Failed to append remark'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to append remark'))
    },
  })

  const trimmed = value.trim()
  const canSubmit = trimmed !== '' && !mutation.isPending

  const handleSubmit = () => {
    if (!canSubmit) return
    mutation.mutate(trimmed)
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{props.title}</DialogTitle>
          <DialogDescription>{props.description}</DialogDescription>
        </DialogHeader>

        {props.currentRemark && (
          <div className='bg-muted/40 rounded-lg border p-3'>
            <p className='text-muted-foreground mb-1 text-xs'>
              {t('Current remark')}
            </p>
            <p className='text-sm whitespace-pre-wrap break-words'>
              {props.currentRemark}
            </p>
          </div>
        )}

        <Textarea
          value={value}
          onChange={(e) => setValue(e.target.value)}
          rows={4}
          placeholder={t('Enter remark to append to the user')}
          autoFocus
        />

        <DialogFooter>
          <DialogClose render={<Button variant='outline' disabled={mutation.isPending} />}>
            {t('Cancel')}
          </DialogClose>
          <Button onClick={handleSubmit} disabled={!canSubmit}>
            {mutation.isPending ? t('Saving...') : t('Append remark')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
