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
import { zodResolver } from '@hookform/resolvers/zod'
import { Layers3, Loader2, Search } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm, useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { getChannels } from '@/features/channels/api'
import type { Channel } from '@/features/channels/types'

import {
  SettingsControlChildren,
  SettingsControlGroup,
  SettingsForm,
  SettingsFormGrid,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

const RELAY_BATCH_SPLIT_OPTION_KEY = 'relay_batch_split.config'
const MAX_BATCH_CONCURRENCY = 4
const MAX_EMBEDDING_ITEMS = 1000
const MAX_RERANK_ITEMS = 200

const batchKindSchema = (hardMaximum: number) =>
  z.object({
    enabled: z.boolean(),
    batch_size: z.coerce.number().int().min(1).max(hardMaximum),
    concurrency: z.coerce.number().int().min(1).max(MAX_BATCH_CONCURRENCY),
    max_items: z.coerce.number().int().min(1).max(hardMaximum),
  })

const relayBatchSplitSchema = z
  .object({
    version: z.literal(1),
    enabled: z.boolean(),
    channel_ids: z.array(z.number().int().positive()),
    embedding: batchKindSchema(MAX_EMBEDDING_ITEMS),
    rerank: batchKindSchema(MAX_RERANK_ITEMS),
  })
  .superRefine((value, context) => {
    for (const kind of ['embedding', 'rerank'] as const) {
      if (value[kind].max_items < value[kind].batch_size) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          path: [kind, 'max_items'],
          message: 'Maximum items must be greater than or equal to batch size.',
        })
      }
    }
    if (value.enabled && value.channel_ids.length === 0) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['channel_ids'],
        message: 'Select at least one channel before enabling batch splitting.',
      })
    }
    if (value.enabled && !value.embedding.enabled && !value.rerank.enabled) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['embedding', 'enabled'],
        message: 'Enable embedding or rerank splitting.',
      })
    }
  })

type RelayBatchSplitInput = z.input<typeof relayBatchSplitSchema>
type RelayBatchSplitValues = z.output<typeof relayBatchSplitSchema>

const defaultConfig: RelayBatchSplitValues = {
  version: 1,
  enabled: false,
  channel_ids: [],
  embedding: {
    enabled: true,
    batch_size: 25,
    concurrency: 2,
    max_items: MAX_EMBEDDING_ITEMS,
  },
  rerank: {
    enabled: false,
    batch_size: 25,
    concurrency: 1,
    max_items: MAX_RERANK_ITEMS,
  },
}

function parseConfig(raw?: string): RelayBatchSplitValues {
  try {
    const result = relayBatchSplitSchema.safeParse(JSON.parse(raw || '{}'))
    return result.success ? result.data : structuredClone(defaultConfig)
  } catch {
    return structuredClone(defaultConfig)
  }
}

async function loadAllChannels(): Promise<Channel[]> {
  const pageSize = 100
  const channels: Channel[] = []
  for (let page = 1; page <= 100; page += 1) {
    const response = await getChannels({ p: page, page_size: pageSize })
    if (!response.success || !response.data) {
      throw new Error(response.message || 'Failed to load channels')
    }
    channels.push(...response.data.items)
    if (
      channels.length >= response.data.total ||
      response.data.items.length < pageSize
    ) {
      break
    }
  }
  return channels
}

type Props = {
  defaultValue?: string
}

export function RelayBatchSplitSection({ defaultValue }: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const defaults = useMemo(() => parseConfig(defaultValue), [defaultValue])
  const baselineRef = useRef(JSON.stringify(defaults))
  const [channels, setChannels] = useState<Channel[]>([])
  const [channelsLoading, setChannelsLoading] = useState(true)
  const [selectorOpen, setSelectorOpen] = useState(false)

  const form = useForm<RelayBatchSplitInput, unknown, RelayBatchSplitValues>({
    resolver: zodResolver(relayBatchSplitSchema),
    defaultValues: defaults,
  })

  useEffect(() => {
    const serialized = JSON.stringify(defaults)
    if (serialized === baselineRef.current) return
    baselineRef.current = serialized
    form.reset(defaults)
  }, [defaults, form])

  useEffect(() => {
    let cancelled = false
    setChannelsLoading(true)
    void loadAllChannels()
      .then((items) => {
        if (!cancelled) setChannels(items)
      })
      .catch((error: Error) => {
        if (!cancelled) toast.error(error.message)
      })
      .finally(() => {
        if (!cancelled) setChannelsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const enabled = form.watch('enabled')
  const selectedChannelIds = form.watch('channel_ids')

  const onSubmit = async (values: RelayBatchSplitValues) => {
    const normalized = {
      ...values,
      channel_ids: [...new Set(values.channel_ids)].sort((a, b) => a - b),
    }
    const serialized = JSON.stringify(normalized)
    if (serialized === baselineRef.current) {
      toast.info(t('No changes to save'))
      return
    }
    const response = await updateOption.mutateAsync({
      key: RELAY_BATCH_SPLIT_OPTION_KEY,
      value: serialized,
    })
    if (!response.success) return
    baselineRef.current = serialized
    form.reset(normalized)
  }

  return (
    <SettingsSection title={t('Embedding & Rerank Batching')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable request batching')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Only explicitly selected channels are affected. Other upstream channels keep their original behavior.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='channel_ids'
            render={() => (
              <FormItem data-settings-form-span='full'>
                <FormLabel>{t('Affected channels')}</FormLabel>
                <div className='flex flex-wrap items-center gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    onClick={() => setSelectorOpen(true)}
                  >
                    {channelsLoading ? (
                      <Loader2 className='h-4 w-4 animate-spin' />
                    ) : (
                      <Layers3 className='h-4 w-4' />
                    )}
                    {t('Select channels')}
                  </Button>
                  <span className='text-muted-foreground text-sm'>
                    {t('{{count}} channel(s) selected', {
                      count: selectedChannelIds.length,
                    })}
                  </span>
                </div>
                {selectedChannelIds.length > 0 && (
                  <div className='flex flex-wrap gap-1.5'>
                    {selectedChannelIds.map((id) => (
                      <span
                        key={id}
                        className='bg-muted rounded-md px-2 py-1 text-xs'
                      >
                        #{id}{' '}
                        {channels.find((channel) => channel.id === id)?.name ||
                          t('Unknown channel')}
                      </span>
                    ))}
                  </div>
                )}
                <FormDescription>
                  {t(
                    'Selection is based only on channel IDs; Base URL and channel name are never used for automatic matching.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <BatchKindFields
            kind='embedding'
            title={t('Embedding batching')}
            description={t(
              'Splits multi-input embedding requests and restores result ordering before returning one response.'
            )}
            hardMaximum={MAX_EMBEDDING_ITEMS}
            disabled={!enabled}
          />
          <BatchKindFields
            kind='rerank'
            title={t('Rerank batching')}
            description={t(
              'Splits documents, merges scores globally, then applies the original top_n and return_documents options.'
            )}
            hardMaximum={MAX_RERANK_ITEMS}
            disabled={!enabled}
          />

          <div className='bg-muted/30 text-muted-foreground rounded-lg border px-3 py-2.5 text-xs leading-relaxed'>
            {t(
              'Every split chunk is an actual upstream request and therefore participates in the channel concurrency, RPM, and queue limits. Logs store only batching metadata, while existing content-audit behavior remains unchanged.'
            )}
          </div>
        </SettingsForm>
      </Form>

      <BatchChannelSelector
        open={selectorOpen}
        onOpenChange={setSelectorOpen}
        channels={channels}
        selectedIds={selectedChannelIds}
        onConfirm={(ids) =>
          form.setValue('channel_ids', ids, {
            shouldDirty: true,
            shouldValidate: true,
          })
        }
      />
    </SettingsSection>
  )
}

type BatchKindFieldsProps = {
  kind: 'embedding' | 'rerank'
  title: string
  description: string
  hardMaximum: number
  disabled: boolean
}

function BatchKindFields(props: BatchKindFieldsProps) {
  const { t } = useTranslation()
  const form = useFormContextForBatch()
  const kindEnabled = form.watch(`${props.kind}.enabled`)
  const controlsDisabled = props.disabled || !kindEnabled

  return (
    <SettingsControlGroup>
      <FormField
        control={form.control}
        name={`${props.kind}.enabled`}
        render={({ field }) => (
          <SettingsSwitchItem className='py-0'>
            <SettingsSwitchContent>
              <FormLabel>{props.title}</FormLabel>
              <FormDescription>{props.description}</FormDescription>
            </SettingsSwitchContent>
            <FormControl>
              <Switch
                checked={field.value}
                onCheckedChange={field.onChange}
                disabled={props.disabled}
              />
            </FormControl>
          </SettingsSwitchItem>
        )}
      />
      <SettingsControlChildren>
        <SettingsFormGrid>
          <BatchNumberField
            name={`${props.kind}.batch_size`}
            label={t('Items per upstream request')}
            min={1}
            max={props.hardMaximum}
            disabled={controlsDisabled}
          />
          <BatchNumberField
            name={`${props.kind}.concurrency`}
            label={t('Parallel batch requests')}
            min={1}
            max={MAX_BATCH_CONCURRENCY}
            disabled={controlsDisabled}
          />
          <BatchNumberField
            name={`${props.kind}.max_items`}
            label={t('Maximum items per client request')}
            min={1}
            max={props.hardMaximum}
            disabled={controlsDisabled}
          />
        </SettingsFormGrid>
      </SettingsControlChildren>
    </SettingsControlGroup>
  )
}

function useFormContextForBatch() {
  // Kept as a small typed wrapper so field path literals remain checked.
  return useFormContext<RelayBatchSplitInput, unknown, RelayBatchSplitValues>()
}

type BatchNumberFieldProps = {
  name:
    | 'embedding.batch_size'
    | 'embedding.concurrency'
    | 'embedding.max_items'
    | 'rerank.batch_size'
    | 'rerank.concurrency'
    | 'rerank.max_items'
  label: string
  min: number
  max: number
  disabled: boolean
}

function BatchNumberField(props: BatchNumberFieldProps) {
  const form = useFormContextForBatch()
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
              min={props.min}
              max={props.max}
              step={1}
              {...safeNumberFieldProps(field)}
              disabled={props.disabled}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

type BatchChannelSelectorProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channels: Channel[]
  selectedIds: number[]
  onConfirm: (ids: number[]) => void
}

function BatchChannelSelector(props: BatchChannelSelectorProps) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [draftIds, setDraftIds] = useState<number[]>(props.selectedIds)

  useEffect(() => {
    if (props.open) setDraftIds(props.selectedIds)
  }, [props.open, props.selectedIds])

  const visibleChannels = useMemo(() => {
    const needle = search.trim().toLowerCase()
    if (!needle) return props.channels
    return props.channels.filter(
      (channel) =>
        channel.name.toLowerCase().includes(needle) ||
        String(channel.base_url || '')
          .toLowerCase()
          .includes(needle) ||
        String(channel.id).includes(needle)
    )
  }, [props.channels, search])

  const toggle = (channelId: number, checked: boolean) => {
    setDraftIds((current) => {
      if (checked) return [...new Set([...current, channelId])]
      return current.filter((id) => id !== channelId)
    })
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Select batching channels')}
      description={t(
        'Batch splitting will apply only to the channel IDs selected here.'
      )}
      contentClassName='max-w-2xl'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => {
              props.onConfirm([...draftIds].sort((a, b) => a - b))
              props.onOpenChange(false)
            }}
          >
            {t('Confirm Selection')}
          </Button>
        </>
      }
    >
      <div className='space-y-3'>
        <div className='relative'>
          <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2' />
          <Input
            className='ps-9'
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('Search by channel ID, name, or Base URL')}
          />
        </div>
        <div className='max-h-[55vh] space-y-1 overflow-y-auto rounded-lg border p-2'>
          {visibleChannels.length === 0 ? (
            <div className='text-muted-foreground py-8 text-center text-sm'>
              {t('No channels found')}
            </div>
          ) : (
            visibleChannels.map((channel) => (
              <label
                key={channel.id}
                className='hover:bg-muted/50 flex cursor-pointer items-start gap-3 rounded-md px-2 py-2'
              >
                <Checkbox
                  checked={draftIds.includes(channel.id)}
                  onCheckedChange={(value) =>
                    toggle(channel.id, value === true)
                  }
                />
                <span className='min-w-0 flex-1'>
                  <span className='block text-sm font-medium'>
                    #{channel.id} · {channel.name}
                  </span>
                  <span className='text-muted-foreground block truncate text-xs'>
                    {channel.base_url || t('Default Base URL')}
                  </span>
                </span>
              </label>
            ))
          )}
        </div>
      </div>
    </Dialog>
  )
}
