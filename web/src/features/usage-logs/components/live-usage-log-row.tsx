/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import {
  flexRender,
  type Row,
  type Table as TanstackTable,
} from '@tanstack/react-table'
import { motion } from 'motion/react'
import { memo, type ReactNode } from 'react'

import {
  TruncatedCell,
  type DataTableColumnClassName,
} from '@/components/data-table'
import { TableCell } from '@/components/ui/table'
import { cn } from '@/lib/utils'

const MotionTableRow = motion.tr
const tableRowClassName =
  'group data-[state=selected]:bg-muted border-b transition-colors hover:[background-color:color-mix(in_oklch,var(--muted)_50%,var(--background))] has-aria-expanded:[background-color:color-mix(in_oklch,var(--muted)_50%,var(--background))]'

interface LiveUsageLogRowProps<TData> {
  row: Row<TData>
  className?: string
  getColumnClassName?: DataTableColumnClassName
  cellRenderColumns: TanstackTable<TData>['options']['columns']
  isEntering: boolean
  visibleColumnIds: string
}

function LiveUsageLogRowInner<TData>(props: LiveUsageLogRowProps<TData>) {
  void props.cellRenderColumns
  void props.visibleColumnIds

  return (
    <MotionTableRow
      layout='position'
      initial={props.isEntering ? { opacity: 0, y: -14 } : false}
      animate={{ opacity: 1, y: 0 }}
      transition={{
        layout: { type: 'spring', stiffness: 260, damping: 30, mass: 0.65 },
        opacity: { duration: 0.18, ease: 'easeOut' },
        y: { type: 'spring', stiffness: 340, damping: 28, mass: 0.5 },
      }}
      data-slot='table-row'
      data-state={props.row.getIsSelected() ? 'selected' : undefined}
      className={cn(tableRowClassName, props.className)}
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
            data-column-id={cell.column.id}
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

const MemoizedLiveUsageLogRow = memo(
  LiveUsageLogRowInner,
  (previous, next) =>
    previous.row === next.row &&
    previous.className === next.className &&
    previous.getColumnClassName === next.getColumnClassName &&
    previous.cellRenderColumns === next.cellRenderColumns &&
    previous.visibleColumnIds === next.visibleColumnIds &&
    previous.isEntering === next.isEntering
) as typeof LiveUsageLogRowInner

export function LiveUsageLogRow<TData>(
  props: Omit<LiveUsageLogRowProps<TData>, 'visibleColumnIds'>
) {
  const visibleColumnIds = props.row
    .getVisibleCells()
    .map((cell) => cell.column.id)
    .join('\0')

  return (
    <MemoizedLiveUsageLogRow {...props} visibleColumnIds={visibleColumnIds} />
  )
}

function getPrimitiveTextContent(content: ReactNode): string | null {
  if (typeof content === 'string' || typeof content === 'number') {
    return String(content)
  }
  return null
}
