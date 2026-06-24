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
 * Shared time range filter for operations stats tabs.
 */
import { useState } from 'react'
import { CalendarDays } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { TIME_RANGE_PRESETS } from '../constants'
import { dateToTimestampSeconds, formatTimestamp } from '../lib/utils'

export interface TimeRangeFilterValue {
  range_days: number
  start_timestamp?: number
  end_timestamp?: number
}

interface TimeRangeFilterProps {
  value: TimeRangeFilterValue
  onChange: (value: TimeRangeFilterValue) => void
  className?: string
}

export function TimeRangeFilter(props: TimeRangeFilterProps) {
  const { t } = useTranslation()
  const { value, onChange, className } = props
  const [open, setOpen] = useState(false)

  const startDate = value.start_timestamp
    ? new Date(value.start_timestamp * 1000)
    : undefined
  const endDate = value.end_timestamp
    ? new Date(value.end_timestamp * 1000)
    : undefined

  const [draftStart, setDraftStart] = useState(
    toDatetimeLocalValue(startDate) ?? ''
  )
  const [draftEnd, setDraftEnd] = useState(toDatetimeLocalValue(endDate) ?? '')

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setDraftStart(toDatetimeLocalValue(startDate) ?? '')
      setDraftEnd(toDatetimeLocalValue(endDate) ?? '')
    }
    setOpen(nextOpen)
  }

  const applyPreset = (days: number) => {
    onChange({ range_days: days })
    setOpen(false)
  }

  const applyCustom = () => {
    const start = parseDatetimeLocal(draftStart)
    const end = parseDatetimeLocal(draftEnd)
    if (start && end && end > start) {
      onChange({
        range_days: value.range_days,
        start_timestamp: dateToTimestampSeconds(start),
        end_timestamp: dateToTimestampSeconds(end),
      })
      setOpen(false)
    }
  }

  const isCustom = Boolean(value.start_timestamp && value.end_timestamp)
  const label = isCustom
    ? `${formatTimestamp(value.start_timestamp!, { second: undefined })} ~ ${formatTimestamp(value.end_timestamp!, { second: undefined })}`
    : t(
        TIME_RANGE_PRESETS.find((p) => p.value === value.range_days)
          ?.labelKey ?? 'Custom'
      )

  return (
    <div className={cn('flex items-center gap-2', className)}>
      <div className='bg-muted inline-flex items-center rounded-lg p-0.5'>
        {TIME_RANGE_PRESETS.map((preset) => (
          <Button
            key={preset.value}
            variant={
              !isCustom && value.range_days === preset.value
                ? 'secondary'
                : 'ghost'
            }
            size='sm'
            onClick={() => applyPreset(preset.value)}
          >
            {t(preset.labelKey)}
          </Button>
        ))}
        <Popover open={open} onOpenChange={handleOpenChange}>
          <PopoverTrigger
            render={
              <Button
                variant={isCustom ? 'secondary' : 'ghost'}
                size='sm'
                className='gap-1'
              />
            }
          >
            <CalendarDays className='size-3.5' />
            {t('Custom')}
          </PopoverTrigger>
          <PopoverContent className='w-auto p-3' align='start'>
            <div className='space-y-3'>
              <div className='space-y-1.5'>
                <label className='text-muted-foreground text-xs'>
                  {t('Start')}
                </label>
                <Input
                  type='datetime-local'
                  value={draftStart}
                  onChange={(e) => setDraftStart(e.target.value)}
                  className='w-[260px]'
                />
              </div>
              <div className='space-y-1.5'>
                <label className='text-muted-foreground text-xs'>
                  {t('End')}
                </label>
                <Input
                  type='datetime-local'
                  value={draftEnd}
                  onChange={(e) => setDraftEnd(e.target.value)}
                  className='w-[260px]'
                />
              </div>
              <div className='flex justify-end gap-2'>
                <Button
                  variant='ghost'
                  size='sm'
                  onClick={() => {
                    setDraftStart(toDatetimeLocalValue(startDate) ?? '')
                    setDraftEnd(toDatetimeLocalValue(endDate) ?? '')
                    setOpen(false)
                  }}
                >
                  {t('Cancel')}
                </Button>
                <Button size='sm' onClick={applyCustom}>
                  {t('Apply')}
                </Button>
              </div>
            </div>
          </PopoverContent>
        </Popover>
      </div>
      {isCustom && (
        <span className='text-muted-foreground hidden text-xs sm:inline'>
          {label}
        </span>
      )}
    </div>
  )
}

function toDatetimeLocalValue(date?: Date): string | undefined {
  if (!date) return undefined
  const pad = (n: number) => String(n).padStart(2, '0')
  const yyyy = date.getFullYear()
  const mm = pad(date.getMonth() + 1)
  const dd = pad(date.getDate())
  const hh = pad(date.getHours())
  const min = pad(date.getMinutes())
  return `${yyyy}-${mm}-${dd}T${hh}:${min}`
}

function parseDatetimeLocal(value: string): Date | undefined {
  if (!value) return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date
}
