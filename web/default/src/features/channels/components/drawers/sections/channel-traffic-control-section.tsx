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
import type { UseFormReturn } from 'react-hook-form'
import { Gauge } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
  sideDrawerSwitchItemClassName,
} from '@/components/drawer-layout'
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

import type { ChannelFormValues } from '../../../lib'

type ChannelTrafficControlSectionProps = {
  form: UseFormReturn<ChannelFormValues>
}

export function ChannelTrafficControlSection({
  form,
}: ChannelTrafficControlSectionProps) {
  const { t } = useTranslation()
  const enabled = form.watch('traffic_control_enabled') === true

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title={t('Channel Traffic Control')}
        description={t(
          'Limit concurrency and requests per minute for this channel with a bounded waiting queue.'
        )}
        icon={<Gauge className='h-4 w-4' aria-hidden='true' />}
      />

      <FormField
        control={form.control}
        name='traffic_control_enabled'
        render={({ field }) => (
          <FormItem className={sideDrawerSwitchItemClassName()}>
            <div className='flex flex-col gap-0.5'>
              <FormLabel>{t('Enable channel traffic control')}</FormLabel>
              <FormDescription className='text-xs'>
                {t(
                  'Limits apply to actual upstream requests, including retries and split batches. Streaming requests hold a concurrency slot until completion.'
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
          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='traffic_max_concurrency'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Maximum concurrency')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={100000}
                      step={1}
                      {...field}
                      onChange={(event) =>
                        field.onChange(Number(event.target.value))
                      }
                    />
                  </FormControl>
                  <FormDescription>{t('0 means unlimited.')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='traffic_rpm'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Requests per minute')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={10000000}
                      step={1}
                      {...field}
                      onChange={(event) =>
                        field.onChange(Number(event.target.value))
                      }
                    />
                  </FormControl>
                  <FormDescription>{t('0 means unlimited.')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='traffic_queue_size'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Queue capacity')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={10000}
                      step={1}
                      {...field}
                      onChange={(event) =>
                        field.onChange(Number(event.target.value))
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('0 rejects immediately when the channel is busy.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='traffic_queue_timeout_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Queue timeout (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={3600}
                      step={1}
                      {...field}
                      onChange={(event) =>
                        field.onChange(Number(event.target.value))
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Requests leave the queue with a service unavailable error after this time.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='border-border/60 bg-muted/30 text-muted-foreground rounded-lg border px-3 py-2.5 text-xs leading-relaxed'>
            {t(
              'Redis coordinates limits across nodes. Without Redis, each instance maintains its own queue.'
            )}
          </div>
        </div>
      )}
    </SideDrawerSection>
  )
}
