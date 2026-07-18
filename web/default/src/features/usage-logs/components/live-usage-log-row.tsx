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
import {
  flexRender,
  type Row,
  type Table as TanstackTable,
} from '@tanstack/react-table'
import { motion } from 'motion/react'
import { memo } from 'react'

import {
  TruncatedCell,
  type DataTableColumnClassName,
} from '@/components/data-table'
import { TableCell, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'

const MotionTableRow = motion.create(TableRow)
const LIVE_ROW_EASE = [0.22, 1, 0.36, 1] as const

interface LiveUsageLogRowProps<TData> {
  row: Row<TData>
  className?: string
  getColumnClassName?: DataTableColumnClassName
  cellRenderColumns: TanstackTable<TData>['options']['columns']
  isEntering: boolean
}

function LiveUsageLogRowInner<TData>(props: LiveUsageLogRowProps<TData>) {
  void props.cellRenderColumns
  const delay = Math.min(props.row.index, 8) * 0.035

  return (
    <MotionTableRow
      layout='position'
      initial={props.isEntering ? { opacity: 0, y: -22 } : false}
      animate={{ opacity: 1, y: 0 }}
      transition={{
        layout: { duration: 0.38, ease: LIVE_ROW_EASE },
        opacity: { duration: 0.24, delay },
        y: { duration: 0.38, delay, ease: LIVE_ROW_EASE },
      }}
      data-state={props.row.getIsSelected() ? 'selected' : undefined}
      className={props.className}
    >
      {props.row.getVisibleCells().map((cell) => {
        const content = flexRender(
          cell.column.columnDef.cell,
          cell.getContext()
        )
        const primitiveText = getPrimitiveTextContent(content)

        return (
          <TableCell
            key={cell.id}
            className={cn(
              'max-w-full min-w-0',
              primitiveText !== null && 'overflow-hidden',
              props.getColumnClassName?.(cell.column.id, 'cell')
            )}
          >
            {primitiveText === null ? (
              content
            ) : (
              <TruncatedCell tooltipContent={primitiveText}>
                {content}
              </TruncatedCell>
            )}
          </TableCell>
        )
      })}
    </MotionTableRow>
  )
}

export const LiveUsageLogRow = memo(
  LiveUsageLogRowInner,
  (previous, next) =>
    previous.row === next.row &&
    previous.className === next.className &&
    previous.getColumnClassName === next.getColumnClassName &&
    previous.cellRenderColumns === next.cellRenderColumns &&
    previous.isEntering === next.isEntering
) as typeof LiveUsageLogRowInner

function getPrimitiveTextContent(content: React.ReactNode): string | null {
  if (typeof content === 'string' || typeof content === 'number') {
    return String(content)
  }

  return null
}
