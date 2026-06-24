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
 * Compact pagination controls used by the screening / block-log tables.
 */
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface ListPaginationProps {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
  className?: string
}

export function ListPagination(props: ListPaginationProps) {
  const { t } = useTranslation()
  const { page, pageSize, total, onPageChange, className } = props

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const startIdx = total === 0 ? 0 : (page - 1) * pageSize + 1
  const endIdx = Math.min(page * pageSize, total)

  return (
    <div
      className={cn(
        'flex flex-wrap items-center justify-between gap-2 border-t pt-2',
        className
      )}
    >
      <span className='text-muted-foreground text-xs tabular-nums'>
        {startIdx}-{endIdx} {t('of')} {total.toLocaleString()}
      </span>
      <div className='flex items-center gap-1'>
        <Button
          variant='outline'
          size='icon-xs'
          onClick={() => onPageChange(1)}
          disabled={page <= 1}
          aria-label={t('First page')}
        >
          <ChevronsLeft className='size-3.5' />
        </Button>
        <Button
          variant='outline'
          size='icon-xs'
          onClick={() => onPageChange(page - 1)}
          disabled={page <= 1}
          aria-label={t('Previous page')}
        >
          <ChevronLeft className='size-3.5' />
        </Button>
        <span className='text-sm tabular-nums px-1'>
          {page} / {totalPages}
        </span>
        <Button
          variant='outline'
          size='icon-xs'
          onClick={() => onPageChange(page + 1)}
          disabled={page >= totalPages}
          aria-label={t('Next page')}
        >
          <ChevronRight className='size-3.5' />
        </Button>
        <Button
          variant='outline'
          size='icon-xs'
          onClick={() => onPageChange(totalPages)}
          disabled={page >= totalPages}
          aria-label={t('Last page')}
        >
          <ChevronsRight className='size-3.5' />
        </Button>
      </div>
    </div>
  )
}
