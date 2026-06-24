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
 * Shared panel showing users behind an IP or User Agent.
 */
import {
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from '@/components/ui/drawer'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatCompactNumber } from '@/lib/format'
import { formatTimestamp } from '../lib/utils'

export interface UsersPanelItem {
  user_id: number
  username: string
  display_name: string
  remark: string
  count: number
  last_seen: number
}

interface UsersPanelProps {
  title: string
  description: string
  open: boolean
  onOpenChange: (open: boolean) => void
  items: UsersPanelItem[]
  total: number
  page: number
  pageSize: number
  onPageChange: (page: number) => void
  isLoading: boolean
  emptyTitle: string
  emptyDescription: string
}

export function UsersPanel(props: UsersPanelProps) {
  const { t } = useTranslation()
  const {
    title,
    description,
    open,
    onOpenChange,
    items,
    total,
    page,
    pageSize,
    onPageChange,
    isLoading,
    emptyTitle,
    emptyDescription,
  } = props

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const startIdx = total === 0 ? 0 : (page - 1) * pageSize + 1
  const endIdx = Math.min(page * pageSize, total)

  return (
    <Drawer open={open} onOpenChange={onOpenChange} direction='right'>
      <DrawerContent className='w-full sm:max-w-md'>
        <DrawerHeader className='border-b'>
          <DrawerTitle className='truncate'>{title}</DrawerTitle>
          <DrawerDescription className='text-muted-foreground text-xs'>
            {description}
          </DrawerDescription>
        </DrawerHeader>

        <div className='flex min-h-0 flex-1 flex-col overflow-hidden p-4'>
          {isLoading ? (
            <div className='text-muted-foreground flex flex-1 items-center justify-center text-sm'>
              {t('Loading...')}
            </div>
          ) : items.length === 0 ? (
            <div className='flex flex-1 flex-col items-center justify-center gap-2 text-center'>
              <p className='text-base font-medium'>{emptyTitle}</p>
              <p className='text-muted-foreground max-w-[260px] text-sm'>
                {emptyDescription}
              </p>
            </div>
          ) : (
            <div className='flex min-h-0 flex-1 flex-col gap-3 overflow-hidden'>
              <div className='border rounded-lg overflow-hidden flex-1 min-h-0 overflow-auto'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('User')}</TableHead>
                      <TableHead className='text-right'>{t('Count')}</TableHead>
                      <TableHead>{t('Last Seen')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {items.map((item) => (
                      <TableRow key={item.user_id}>
                        <TableCell>
                          <div className='flex flex-col'>
                            <span className='font-medium'>
                              {item.username || `ID: ${item.user_id}`}
                            </span>
                            {item.display_name && (
                              <span className='text-muted-foreground text-xs'>
                                {item.display_name}
                              </span>
                            )}
                            {item.remark && (
                              <span className='text-muted-foreground text-xs'>
                                {item.remark}
                              </span>
                            )}
                          </div>
                        </TableCell>
                        <TableCell className='text-right tabular-nums'>
                          {formatCompactNumber(item.count)}
                        </TableCell>
                        <TableCell className='text-muted-foreground text-xs'>
                          {item.last_seen
                            ? formatTimestamp(item.last_seen, {
                                year: undefined,
                                month: '2-digit',
                                day: '2-digit',
                                hour: '2-digit',
                                minute: '2-digit',
                              })
                            : '-'}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>

              {total > pageSize && (
                <div className='flex items-center justify-between gap-2 border-t pt-2'>
                  <span className='text-muted-foreground text-xs'>
                    {startIdx}-{endIdx} {t('of')} {total.toLocaleString()}
                  </span>
                  <div className='flex items-center gap-1'>
                    <Button
                      variant='outline'
                      size='icon-xs'
                      onClick={() => onPageChange(1)}
                      disabled={page <= 1}
                    >
                      <ChevronsLeft className='size-3.5' />
                    </Button>
                    <Button
                      variant='outline'
                      size='icon-xs'
                      onClick={() => onPageChange(page - 1)}
                      disabled={page <= 1}
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
                    >
                      <ChevronRight className='size-3.5' />
                    </Button>
                    <Button
                      variant='outline'
                      size='icon-xs'
                      onClick={() => onPageChange(totalPages)}
                      disabled={page >= totalPages}
                    >
                      <ChevronsRight className='size-3.5' />
                    </Button>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        <DrawerFooter className='border-t'>
          <DrawerClose asChild>
            <Button variant='outline'>{t('Close')}</Button>
          </DrawerClose>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  )
}
