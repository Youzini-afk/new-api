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
 * Simple rank table used by IP Stats, User Agents and User Leaderboard tabs.
 */
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
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

interface RankTableProps<T> {
  items: T[]
  columns: {
    key: string
    title: string
    className?: string
    align?: 'left' | 'right' | 'center'
    render: (item: T, index: number) => React.ReactNode
  }[]
  isLoading: boolean
  error: Error | null
  emptyTitle: string
  emptyDescription: string
  onRetry?: () => void
  className?: string
}

export function RankTable<T>(props: RankTableProps<T>) {
  const { t } = useTranslation()
  const {
    items,
    columns,
    isLoading,
    error,
    emptyTitle,
    emptyDescription,
    onRetry,
    className,
  } = props

  if (error) {
    return (
      <ErrorState
        title={t('Failed to load')}
        description={error.message}
        onRetry={onRetry}
        className='min-h-[240px]'
      />
    )
  }

  return (
    <div
      className={cn(
        'bg-card ring-foreground/10 flex flex-col overflow-hidden rounded-xl ring-1',
        className
      )}
    >
      <div className='min-h-0 flex-1 overflow-auto'>
        {isLoading && items.length === 0 ? (
          <div className='flex min-h-[240px] flex-col items-center justify-center gap-3'>
            <Loader2 className='text-muted-foreground size-6 animate-spin' />
            <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
          </div>
        ) : items.length === 0 ? (
          <EmptyState
            title={emptyTitle}
            description={emptyDescription}
            className='min-h-[240px]'
          />
        ) : (
          <Table>
            <TableHeader className='bg-muted/50 sticky top-0 z-10'>
              <TableRow>
                {columns.map((col) => (
                  <TableHead
                    key={col.key}
                    className={cn(
                      col.align === 'right' && 'text-right',
                      col.align === 'center' && 'text-center',
                      col.className
                    )}
                  >
                    {col.title}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item, index) => (
                <TableRow key={index}>
                  {columns.map((col) => (
                    <TableCell
                      key={col.key}
                      className={cn(
                        col.align === 'right' && 'text-right',
                        col.align === 'center' && 'text-center',
                        col.className
                      )}
                    >
                      {col.render(item, index)}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>
    </div>
  )
}
