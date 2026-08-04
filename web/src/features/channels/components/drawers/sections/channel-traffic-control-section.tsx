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
import { Gauge } from 'lucide-react'
import { useFormContext } from 'react-hook-form'
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
import {
  MAX_CHANNEL_TRAFFIC_CONCURRENCY,
  MAX_CHANNEL_TRAFFIC_QUEUE_SIZE,
  MAX_CHANNEL_TRAFFIC_QUEUE_TIMEOUT_SECONDS,
  MAX_CHANNEL_TRAFFIC_RPM,
} from '../../../lib/channel-form'

export function ChannelTrafficControlSection() {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
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
            <TrafficNumberField
              name='traffic_max_concurrency'
              label={t('Maximum concurrency')}
              maximum={MAX_CHANNEL_TRAFFIC_CONCURRENCY}
              description={t('0 means unlimited.')}
            />
            <TrafficNumberField
              name='traffic_rpm'
              label={t('Requests per minute')}
              maximum={MAX_CHANNEL_TRAFFIC_RPM}
              description={t('0 means unlimited.')}
            />
            <TrafficNumberField
              name='traffic_queue_size'
              label={t('Queue capacity')}
              maximum={MAX_CHANNEL_TRAFFIC_QUEUE_SIZE}
              description={t('0 rejects immediately when the channel is busy.')}
            />
            <TrafficNumberField
              name='traffic_queue_timeout_seconds'
              label={t('Queue timeout (seconds)')}
              maximum={MAX_CHANNEL_TRAFFIC_QUEUE_TIMEOUT_SECONDS}
              description={t(
                'Requests leave the queue with a service unavailable error after this time.'
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

type TrafficNumberFieldProps = {
  name:
    | 'traffic_max_concurrency'
    | 'traffic_rpm'
    | 'traffic_queue_size'
    | 'traffic_queue_timeout_seconds'
  label: string
  maximum: number
  description: string
}

function TrafficNumberField(props: TrafficNumberFieldProps) {
  const form = useFormContext<ChannelFormValues>()

  return (
    <FormField
      control={form.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{props.label}</FormLabel>
          <FormControl>
            <Input
              type='number'
              min={0}
              max={props.maximum}
              step={1}
              {...field}
              value={field.value ?? 0}
              onChange={(event) => field.onChange(Number(event.target.value))}
            />
          </FormControl>
          <FormDescription>{props.description}</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
