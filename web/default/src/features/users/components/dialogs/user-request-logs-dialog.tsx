import { useQuery } from '@tanstack/react-query'
import { Eye } from 'lucide-react'
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
import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getAllLogs } from '@/features/usage-logs/api'
import { DetailsDialog } from '@/features/usage-logs/components/dialogs/details-dialog'
import { ModelBadge } from '@/features/usage-logs/components/model-badge'
import type { UsageLog } from '@/features/usage-logs/data/schema'
import { formatModelName } from '@/features/usage-logs/lib/format'
import {
  getLogTypeConfig,
  isDisplayableLogType,
} from '@/features/usage-logs/lib/utils'
import {
  formatLogQuota,
  formatTimestampToDate,
  formatUseTime,
} from '@/lib/format'

import type { User } from '../../types'

const PAGE_SIZE = 10

function normalizeLogs(items: unknown[] | undefined): UsageLog[] {
  if (!Array.isArray(items)) return []
  return items as UsageLog[]
}

function UserRequestLogModelCell(props: { log: UsageLog }) {
  if (!isDisplayableLogType(props.log.type)) {
    return <span className='text-muted-foreground text-xs'>-</span>
  }

  const modelInfo = formatModelName(props.log)
  return (
    <ModelBadge
      modelName={modelInfo.name}
      actualModel={modelInfo.actualModel}
    />
  )
}

interface UserRequestLogsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: Pick<User, 'id' | 'username' | 'display_name'>
}

export function UserRequestLogsDialog(props: UserRequestLogsDialogProps) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [detailLog, setDetailLog] = useState<UsageLog | null>(null)

  useEffect(() => {
    if (props.open) setPage(1)
  }, [props.open, props.user.id])

  const query = useQuery({
    queryKey: ['user-request-logs', props.user.id, props.user.username, page],
    enabled: props.open,
    queryFn: async () => {
      const result = await getAllLogs({
        username: props.user.username,
        p: page,
        page_size: PAGE_SIZE,
      })
      if (!result.success) {
        throw new Error(result.message || t('Failed to load request logs'))
      }
      return result.data ?? { items: [], total: 0, page, page_size: PAGE_SIZE }
    },
  })

  useEffect(() => {
    if (query.error) {
      toast.error(query.error.message || t('Failed to load request logs'))
    }
  }, [query.error, t])

  const logs = normalizeLogs(query.data?.items)
  const total = query.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const isLoading = query.isLoading || (query.isFetching && logs.length === 0)
  let tableRows: ReactNode

  if (isLoading) {
    tableRows = (
      <TableRow>
        <TableCell
          colSpan={7}
          className='text-muted-foreground py-8 text-center'
        >
          {t('Loading...')}
        </TableCell>
      </TableRow>
    )
  } else if (logs.length === 0) {
    tableRows = (
      <TableRow>
        <TableCell
          colSpan={7}
          className='text-muted-foreground py-8 text-center'
        >
          <div className='flex flex-col gap-1'>
            <span className='text-foreground font-medium'>
              {t('No request logs found')}
            </span>
            <span>{t('This user has no request logs yet.')}</span>
          </div>
        </TableCell>
      </TableRow>
    )
  } else {
    tableRows = logs.map((log) => {
      const typeConfig = getLogTypeConfig(log.type)
      const typeVariant =
        typeConfig.color === 'default' ? 'neutral' : typeConfig.color
      return (
        <TableRow key={log.id}>
          <TableCell className='text-muted-foreground font-mono text-xs'>
            {formatTimestampToDate(log.created_at)}
          </TableCell>
          <TableCell>
            <StatusBadge
              label={t(typeConfig.label)}
              variant={typeVariant as StatusBadgeProps['variant']}
              size='sm'
              copyable={false}
            />
          </TableCell>
          <TableCell>
            <UserRequestLogModelCell log={log} />
          </TableCell>
          <TableCell className='font-mono text-xs'>
            {isDisplayableLogType(log.type)
              ? `${log.prompt_tokens.toLocaleString()} / ${log.completion_tokens.toLocaleString()}`
              : '-'}
          </TableCell>
          <TableCell className='font-mono text-xs'>
            {formatLogQuota(log.quota)}
          </TableCell>
          <TableCell className='font-mono text-xs'>
            {isDisplayableLogType(log.type) ? formatUseTime(log.use_time) : '-'}
          </TableCell>
          <TableCell className='text-right'>
            <Button
              variant='ghost'
              size='sm'
              className='h-7 px-2'
              onClick={() => setDetailLog(log)}
            >
              <Eye className='size-3.5' aria-hidden='true' />
              <span className='sr-only'>{t('View Details')}</span>
            </Button>
          </TableCell>
        </TableRow>
      )
    })
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={props.onOpenChange}
        title={t('Request Logs')}
        description={t('Recent request logs for {{username}}', {
          username: props.user.username,
        })}
        contentClassName='sm:max-w-5xl'
        bodyClassName='space-y-3'
        contentHeight='min(70vh, 42rem)'
      >
        <div className='flex items-center justify-between gap-3 text-xs'>
          <span className='text-muted-foreground'>
            {t('Total')}: {total.toLocaleString()}
          </span>
          <span className='text-muted-foreground'>
            {t('Page {{current}} of {{total}}', {
              current: page,
              total: totalPages,
            })}
          </span>
        </div>

        <div className='overflow-hidden rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Time')}</TableHead>
                <TableHead>{t('Type')}</TableHead>
                <TableHead>{t('Model')}</TableHead>
                <TableHead>{t('Tokens')}</TableHead>
                <TableHead>{t('Cost')}</TableHead>
                <TableHead>{t('Timing')}</TableHead>
                <TableHead className='text-right'>{t('Details')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>{tableRows}</TableBody>
          </Table>
        </div>

        <div className='flex items-center justify-between gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={page <= 1 || query.isFetching}
            onClick={() => setPage((value) => Math.max(1, value - 1))}
          >
            {t('Previous')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={page >= totalPages || query.isFetching}
            onClick={() => setPage((value) => Math.min(totalPages, value + 1))}
          >
            {t('Next')}
          </Button>
        </div>
      </Dialog>

      {detailLog ? (
        <DetailsDialog
          log={detailLog}
          isAdmin
          open={!!detailLog}
          onOpenChange={(open) => {
            if (!open) setDetailLog(null)
          }}
        />
      ) : null}
    </>
  )
}
