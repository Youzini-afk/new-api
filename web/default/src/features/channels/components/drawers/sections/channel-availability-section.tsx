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
import { useFieldArray, type UseFormReturn } from 'react-hook-form'
import { CalendarClock, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
  sideDrawerSwitchItemClassName,
} from '@/components/drawer-layout'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { COMMON_TIMEZONES } from '@/lib/timezones'

import type { ChannelFormValues } from '../../../lib'
import type { ChannelAvailabilityState } from '../../../types'

const WEEKDAYS = [
  { value: 1, label: 'Mon' },
  { value: 2, label: 'Tue' },
  { value: 3, label: 'Wed' },
  { value: 4, label: 'Thu' },
  { value: 5, label: 'Fri' },
  { value: 6, label: 'Sat' },
  { value: 7, label: 'Sun' },
] as const

const DEFAULT_WINDOW = {
  weekdays: [1, 2, 3, 4, 5, 6, 7],
  start: '00:00',
  end: '08:00',
}

type ChannelAvailabilitySectionProps = {
  form: UseFormReturn<ChannelFormValues>
  savedState?: ChannelAvailabilityState
}

function formatTransition(
  timestamp: number,
  timezone: string | undefined
): string {
  try {
    return new Intl.DateTimeFormat(undefined, {
      month: 'short',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
      timeZone: timezone || undefined,
    }).format(new Date(timestamp * 1000))
  } catch {
    return new Date(timestamp * 1000).toLocaleString()
  }
}

export function ChannelAvailabilitySection({
  form,
  savedState,
}: ChannelAvailabilitySectionProps) {
  const { t } = useTranslation()
  const enabled = form.watch('availability_schedule_enabled') === true
  const { fields, append, remove } = useFieldArray({
    control: form.control,
    name: 'availability_schedule_windows',
  })

  const timezoneOptions = COMMON_TIMEZONES.map((timezone) => ({
    value: timezone.value,
    label: timezone.label,
  }))
  const nextAction =
    savedState?.next_transition_action === 'open' ? t('opens') : t('closes')

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title={t('Scheduled Availability')}
        description={t(
          'Control when this channel can receive new requests without changing its enabled status.'
        )}
        icon={<CalendarClock className='h-4 w-4' aria-hidden='true' />}
      />

      <FormField
        control={form.control}
        name='availability_schedule_enabled'
        render={({ field }) => (
          <FormItem className={sideDrawerSwitchItemClassName()}>
            <div className='flex flex-col gap-0.5'>
              <FormLabel>{t('Enable scheduled availability')}</FormLabel>
              <FormDescription className='text-xs'>
                {t(
                  'Outside the configured windows, new traffic skips this channel. In-flight requests continue.'
                )}
              </FormDescription>
            </div>
            <FormControl>
              <Switch
                checked={field.value === true}
                onCheckedChange={field.onChange}
              />
            </FormControl>
          </FormItem>
        )}
      />

      {enabled && (
        <div className='flex flex-col gap-4'>
          {savedState?.enabled && (
            <div className='border-border/60 bg-muted/30 flex flex-wrap items-center gap-2 rounded-lg border px-3 py-2.5 text-xs'>
              <span className='text-muted-foreground'>
                {t('Current saved state')}:
              </span>
              <StatusBadge
                label={
                  savedState.open ? t('Scheduled Open') : t('Scheduled Closed')
                }
                variant={savedState.open ? 'success' : 'neutral'}
                size='sm'
                copyable={false}
              />
              {savedState.next_transition_at && (
                <span className='text-muted-foreground'>
                  {t('Next transition')}: {nextAction}{' '}
                  {formatTransition(
                    savedState.next_transition_at,
                    savedState.timezone
                  )}
                </span>
              )}
            </div>
          )}

          <FormField
            control={form.control}
            name='availability_schedule_timezone'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Timezone')}</FormLabel>
                <FormControl>
                  <Combobox
                    options={timezoneOptions}
                    value={field.value || 'Asia/Shanghai'}
                    onValueChange={(value) => field.onChange(value || '')}
                    placeholder={t('Select or enter an IANA timezone')}
                    searchPlaceholder={t('Search timezone...')}
                    emptyText={t(
                      'Enter an IANA timezone, for example Asia/Shanghai'
                    )}
                    allowCustomValue
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'IANA timezone names are supported. Daylight-saving changes are handled automatically.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='flex items-center justify-between gap-3'>
            <div>
              <div className='text-sm font-medium'>
                {t('Availability windows')}
              </div>
              <div className='text-muted-foreground text-xs'>
                {t('Start is inclusive and end is exclusive.')}
              </div>
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() =>
                append({
                  ...DEFAULT_WINDOW,
                  weekdays: [...DEFAULT_WINDOW.weekdays],
                })
              }
              disabled={fields.length >= 32}
            >
              <Plus className='mr-1 h-3.5 w-3.5' />
              {t('Add window')}
            </Button>
          </div>

          <div className='flex flex-col gap-3'>
            {fields.map((field, index) => (
              <div
                key={field.id}
                className='border-border/60 flex flex-col gap-3 rounded-lg border p-3'
              >
                <div className='flex items-center justify-between gap-2'>
                  <span className='text-sm font-medium'>
                    {t('Window {{number}}', { number: index + 1 })}
                  </span>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Remove window')}
                    onClick={() => remove(index)}
                  >
                    <Trash2 className='h-4 w-4' />
                  </Button>
                </div>

                <FormField
                  control={form.control}
                  name={`availability_schedule_windows.${index}.weekdays`}
                  render={({ field: weekdayField }) => {
                    const selectedDays = weekdayField.value || []
                    return (
                      <FormItem>
                        <FormLabel>{t('Weekdays')}</FormLabel>
                        <FormControl>
                          <div className='flex flex-wrap gap-1.5'>
                            {WEEKDAYS.map((weekday) => {
                              const selected = selectedDays.includes(
                                weekday.value
                              )
                              return (
                                <Button
                                  key={weekday.value}
                                  type='button'
                                  variant={selected ? 'default' : 'outline'}
                                  size='sm'
                                  className='h-8 min-w-10 px-2 text-xs'
                                  onClick={() => {
                                    const next = selected
                                      ? selectedDays.filter(
                                          (day) => day !== weekday.value
                                        )
                                      : [...selectedDays, weekday.value].sort(
                                          (a, b) => a - b
                                        )
                                    weekdayField.onChange(next)
                                  }}
                                >
                                  {t(weekday.label)}
                                </Button>
                              )
                            })}
                          </div>
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )
                  }}
                />

                <div className='grid grid-cols-2 gap-3'>
                  <FormField
                    control={form.control}
                    name={`availability_schedule_windows.${index}.start`}
                    render={({ field: startField }) => (
                      <FormItem>
                        <FormLabel>{t('Start')}</FormLabel>
                        <FormControl>
                          <Input type='time' step={60} {...startField} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`availability_schedule_windows.${index}.end`}
                    render={({ field: endField }) => (
                      <FormItem>
                        <FormLabel>{t('End')}</FormLabel>
                        <FormControl>
                          <Input type='time' step={60} {...endField} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
                <FormDescription>
                  {t(
                    'If the end is earlier than the start, the window continues into the next day.'
                  )}
                </FormDescription>
              </div>
            ))}
          </div>
        </div>
      )}
    </SideDrawerSection>
  )
}
