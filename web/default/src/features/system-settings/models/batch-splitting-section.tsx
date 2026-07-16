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
import { useQuery } from '@tanstack/react-query'
import { Info, ListChecks, ShieldCheck, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { getChannels } from '@/features/channels/api'
import { CHANNEL_TYPES } from '@/features/channels/constants'
import type { Channel } from '@/features/channels/types'

import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'
import { BatchSplittingChannelDialog } from './batch-splitting-channel-dialog'

const OPTION_KEY = 'relay_batch_split.config'

const batchKindSchema = (hardMax: number) =>
  z
    .object({
      enabled: z.boolean(),
      batch_size: z.number().int().min(1).max(hardMax),
      concurrency: z.number().int().min(1).max(4),
      max_items: z.number().int().min(1).max(hardMax),
    })
    .refine((value) => value.max_items >= value.batch_size, {
      path: ['max_items'],
      message: 'Maximum items must be greater than or equal to batch size',
    })

const relayBatchSplitSchema = z.object({
  version: z.literal(1),
  enabled: z.boolean(),
  channel_ids: z.array(z.number().int().positive()),
  embedding: batchKindSchema(1000),
  rerank: batchKindSchema(200),
})

type RelayBatchSplitValues = z.infer<typeof relayBatchSplitSchema>

const DEFAULT_CONFIG: RelayBatchSplitValues = {
  version: 1,
  enabled: false,
  channel_ids: [],
  embedding: {
    enabled: true,
    batch_size: 25,
    concurrency: 2,
    max_items: 1000,
  },
  rerank: {
    enabled: false,
    batch_size: 25,
    concurrency: 1,
    max_items: 200,
  },
}

function parseRelayBatchSplitConfig(raw: string): RelayBatchSplitValues {
  if (!raw.trim()) return structuredClone(DEFAULT_CONFIG)
  try {
    const result = relayBatchSplitSchema.safeParse(JSON.parse(raw))
    if (result.success) return normalizeRelayBatchSplitConfig(result.data)
  } catch {
    // Invalid persisted values fail closed in the backend; use safe UI defaults.
  }
  return structuredClone(DEFAULT_CONFIG)
}

function normalizeRelayBatchSplitConfig(
  values: RelayBatchSplitValues
): RelayBatchSplitValues {
  return {
    version: 1,
    enabled: values.enabled,
    channel_ids: [...new Set(values.channel_ids)].sort((a, b) => a - b),
    embedding: {
      enabled: values.embedding.enabled,
      batch_size: values.embedding.batch_size,
      concurrency: values.embedding.concurrency,
      max_items: values.embedding.max_items,
    },
    rerank: {
      enabled: values.rerank.enabled,
      batch_size: values.rerank.batch_size,
      concurrency: values.rerank.concurrency,
      max_items: values.rerank.max_items,
    },
  }
}

function serializeRelayBatchSplitConfig(values: RelayBatchSplitValues) {
  return JSON.stringify(normalizeRelayBatchSplitConfig(values))
}

async function loadAllChannels(): Promise<Channel[]> {
  const pageSize = 100
  const channels: Channel[] = []
  let page = 1

  while (page <= 100) {
    const response = await getChannels({
      p: page,
      page_size: pageSize,
      sort_by: 'id',
      sort_order: 'asc',
    })
    if (!response.success) {
      throw new Error(response.message || 'Failed to load channels')
    }
    const items = response.data?.items ?? []
    channels.push(...items)
    const total = response.data?.total ?? channels.length
    if (channels.length >= total || items.length < pageSize) break
    page += 1
  }

  return [...new Map(channels.map((channel) => [channel.id, channel])).values()]
}

type BatchSplittingSectionProps = {
  defaultValue: string
}

export function BatchSplittingSection({
  defaultValue,
}: BatchSplittingSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [channelDialogOpen, setChannelDialogOpen] = useState(false)

  const defaults = useMemo(
    () => parseRelayBatchSplitConfig(defaultValue),
    [defaultValue]
  )
  const baselineRef = useRef(serializeRelayBatchSplitConfig(defaults))

  const form = useForm<RelayBatchSplitValues>({
    resolver: zodResolver(relayBatchSplitSchema),
    defaultValues: defaults,
  })

  useEffect(() => {
    baselineRef.current = serializeRelayBatchSplitConfig(defaults)
    form.reset(defaults)
  }, [defaults, form])

  const channelsQuery = useQuery({
    queryKey: ['channels', 'batch-splitting-selector'],
    queryFn: loadAllChannels,
    staleTime: 30_000,
  })

  const channels = useMemo(() => channelsQuery.data ?? [], [channelsQuery.data])
  const channelById = useMemo(
    () => new Map(channels.map((channel) => [channel.id, channel])),
    [channels]
  )
  const selectedChannelIds = form.watch('channel_ids')
  const globallyEnabled = form.watch('enabled')
  const embeddingBatchSize = form.watch('embedding.batch_size')
  const rerankBatchSize = form.watch('rerank.batch_size')
  const isDirty = form.formState.isDirty

  const setSelectedChannelIds = (channelIds: number[]) => {
    form.setValue('channel_ids', channelIds, {
      shouldDirty: true,
      shouldTouch: true,
      shouldValidate: true,
    })
  }

  const removeChannel = (channelId: number) => {
    setSelectedChannelIds(
      selectedChannelIds.filter((selectedId) => selectedId !== channelId)
    )
  }

  const onSubmit = async (values: RelayBatchSplitValues) => {
    const normalized = normalizeRelayBatchSplitConfig(values)
    const serialized = serializeRelayBatchSplitConfig(normalized)
    if (serialized === baselineRef.current) {
      toast.info(t('No changes to save'))
      return
    }

    const response = await updateOption.mutateAsync({
      key: OPTION_KEY,
      value: serialized,
    })
    if (!response.success) return

    baselineRef.current = serialized
    form.reset(normalized)
  }

  const resetForm = () => form.reset(defaults)

  return (
    <SettingsSection title={t('Embedding & Rerank Batching')}>
      <FormNavigationGuard when={isDirty} />
      <FormDirtyIndicator isDirty={isDirty} />

      <BatchSplittingChannelDialog
        open={channelDialogOpen}
        onOpenChange={setChannelDialogOpen}
        channels={channels}
        selectedChannelIds={selectedChannelIds}
        onConfirm={setSelectedChannelIds}
        isLoading={channelsQuery.isLoading}
        errorMessage={
          channelsQuery.error instanceof Error
            ? channelsQuery.error.message
            : ''
        }
      />

      <Form {...form}>
        <SettingsForm
          onSubmit={form.handleSubmit(onSubmit)}
          className='lg:grid-cols-1'
        >
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            onReset={resetForm}
            isSaving={updateOption.isPending || form.formState.isSubmitting}
            isSaveDisabled={!isDirty}
            isResetDisabled={!isDirty}
          />

          <Alert>
            <Info />
            <AlertTitle>{t('Explicit channel selection only')}</AlertTitle>
            <AlertDescription>
              {t(
                'This feature never detects channels from ai.gitee.com, channel names, or Base URLs. It runs only for the channel IDs selected below.'
              )}
            </AlertDescription>
          </Alert>

          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable request batching')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Split oversized Embedding and Rerank requests before sending them to selected upstream channels.'
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

          <Separator />

          <FormField
            control={form.control}
            name='channel_ids'
            render={({ field }) => (
              <FormItem className='space-y-3'>
                <div className='flex flex-wrap items-center justify-between gap-3'>
                  <div className='space-y-1'>
                    <FormLabel>
                      {t('Channels where batching applies')}
                    </FormLabel>
                    <FormDescription>
                      {t(
                        'Other channels keep their original request and response behavior.'
                      )}
                    </FormDescription>
                  </div>
                  <Button
                    type='button'
                    variant='outline'
                    onClick={() => setChannelDialogOpen(true)}
                  >
                    <ListChecks data-icon='inline-start' />
                    {t('Select channels')}
                  </Button>
                </div>

                <FormControl>
                  <div className='rounded-xl border'>
                    {field.value.length === 0 ? (
                      <div className='text-muted-foreground px-4 py-8 text-center text-sm'>
                        {t('No batching channels selected')}
                      </div>
                    ) : (
                      <div className='divide-y'>
                        {field.value.map((channelId) => {
                          const channel = channelById.get(channelId)
                          const typeName = channel
                            ? (CHANNEL_TYPES[
                                channel.type as keyof typeof CHANNEL_TYPES
                              ] ?? 'Unknown')
                            : 'Unknown'
                          return (
                            <div
                              key={channelId}
                              className='flex min-w-0 items-center gap-3 px-3 py-2.5'
                            >
                              <Badge variant='outline'>#{channelId}</Badge>
                              <div className='min-w-0 flex-1'>
                                <div className='flex min-w-0 flex-wrap items-center gap-2'>
                                  <span className='truncate font-medium'>
                                    {channel?.name ??
                                      t('Missing channel #{{id}}', {
                                        id: channelId,
                                      })}
                                  </span>
                                  {channel ? (
                                    <Badge variant='secondary'>
                                      {t(typeName)}
                                    </Badge>
                                  ) : (
                                    <Badge variant='destructive'>
                                      {t('Missing')}
                                    </Badge>
                                  )}
                                </div>
                                {channel ? (
                                  <p
                                    className='text-muted-foreground truncate font-mono text-xs'
                                    title={channel.base_url ?? ''}
                                  >
                                    {channel.base_url || t('Default Base URL')}
                                  </p>
                                ) : null}
                              </div>
                              <Button
                                type='button'
                                variant='ghost'
                                size='icon-sm'
                                aria-label={t('Remove channel #{{id}}', {
                                  id: channelId,
                                })}
                                onClick={() => removeChannel(channelId)}
                              >
                                <X />
                              </Button>
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </div>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          {globallyEnabled && selectedChannelIds.length === 0 ? (
            <Alert variant='destructive'>
              <AlertTitle>{t('No channel is selected')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Batching is enabled but will not affect any request until at least one channel is selected.'
                )}
              </AlertDescription>
            </Alert>
          ) : null}

          <Separator />

          <div className='grid gap-4 xl:grid-cols-2'>
            <Card>
              <CardHeader>
                <CardTitle>{t('Embedding requests')}</CardTitle>
                <CardDescription>
                  {t(
                    'A flat numeric token array remains one input and is never split. Arrays of strings or token arrays are split by outer item.'
                  )}
                </CardDescription>
                <CardAction>
                  <FormField
                    control={form.control}
                    name='embedding.enabled'
                    render={({ field }) => (
                      <FormItem>
                        <FormControl>
                          <Switch
                            checked={field.value}
                            onCheckedChange={field.onChange}
                            aria-label={t('Enable Embedding batching')}
                          />
                        </FormControl>
                      </FormItem>
                    )}
                  />
                </CardAction>
              </CardHeader>
              <CardContent className='grid gap-4 sm:grid-cols-3'>
                <FormField
                  control={form.control}
                  name='embedding.batch_size'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Items per batch')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          max={1000}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>{t('Recommended: 25')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='embedding.concurrency'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Batch concurrency')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          max={4}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>{t('Range: 1-4')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='embedding.max_items'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Maximum request items')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={embeddingBatchSize}
                          max={1000}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>{t('Maximum: 1000')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>{t('Rerank requests')}</CardTitle>
                <CardDescription>
                  {t(
                    'Each batch is scored independently, then results are globally sorted and the original top_n is applied once.'
                  )}
                </CardDescription>
                <CardAction>
                  <FormField
                    control={form.control}
                    name='rerank.enabled'
                    render={({ field }) => (
                      <FormItem>
                        <FormControl>
                          <Switch
                            checked={field.value}
                            onCheckedChange={field.onChange}
                            aria-label={t('Enable Rerank batching')}
                          />
                        </FormControl>
                      </FormItem>
                    )}
                  />
                </CardAction>
              </CardHeader>
              <CardContent className='grid gap-4 sm:grid-cols-3'>
                <FormField
                  control={form.control}
                  name='rerank.batch_size'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Documents per batch')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          max={200}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>{t('Recommended: 25')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='rerank.concurrency'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Batch concurrency')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          max={4}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>{t('Range: 1-4')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='rerank.max_items'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Maximum request documents')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={rerankBatchSize}
                          max={200}
                          {...safeNumberFieldProps(field)}
                        />
                      </FormControl>
                      <FormDescription>{t('Maximum: 200')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </CardContent>
            </Card>
          </div>

          <Alert>
            <ShieldCheck />
            <AlertTitle>{t('Response and audit behavior')}</AlertTitle>
            <AlertDescription>
              <ul className='list-disc space-y-1 ps-5'>
                <li>
                  {t(
                    'The client receives one merged response. If any subrequest fails, no partial response is sent.'
                  )}
                </li>
                <li>
                  {t(
                    'Quota settlement runs once using the aggregated usage from all successful batches.'
                  )}
                </li>
                <li>
                  {t(
                    'Batch counts and timing are stored in admin metadata. Existing content auditing still records the original complete input, query, and documents, visible only to administrators and Root.'
                  )}
                </li>
              </ul>
            </AlertDescription>
          </Alert>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
