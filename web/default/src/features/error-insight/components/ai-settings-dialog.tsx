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
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { getChannels } from '@/features/channels/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getErrorInsightAISetting, saveErrorInsightAISetting } from '../api'
import type { ErrorInsightAISetting } from '../types'

interface AISettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const defaultSetting: ErrorInsightAISetting = {
  enabled: false,
  channel_id: 0,
  model: '',
  sample_size: 5,
  batch_limit: 10,
  include_original_error: true,
  redact_sensitive: true,
  prompt_template: '',
  json_output_params: {
    response_format: {
      type: 'json_object',
    },
  },
}

export function AISettingsDialog(props: AISettingsDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [form, setForm] = useState<ErrorInsightAISetting>(defaultSetting)
  const [jsonParamsText, setJsonParamsText] = useState(
    JSON.stringify(defaultSetting.json_output_params, null, 2)
  )

  const settingQuery = useQuery({
    queryKey: ['error-insight', 'ai-settings'],
    enabled: props.open,
    queryFn: async () => {
      const result = await getErrorInsightAISetting()
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load AI settings'))
      }
      return result.data
    },
  })

  const channelsQuery = useQuery({
    queryKey: ['channels', 'error-insight-ai'],
    enabled: props.open,
    queryFn: async () => {
      const result = await getChannels({ page_size: 100, status: 'enabled' })
      if (!result.success) {
        throw new Error(result.message || t('Failed to load channels'))
      }
      return result.data?.items ?? []
    },
  })

  useEffect(() => {
    if (!settingQuery.data) return
    setForm(settingQuery.data)
    setJsonParamsText(JSON.stringify(settingQuery.data.json_output_params, null, 2))
  }, [settingQuery.data])

  const selectedChannel = useMemo(
    () => channelsQuery.data?.find((channel) => channel.id === form.channel_id),
    [channelsQuery.data, form.channel_id]
  )

  const modelOptions = useMemo(() => {
    if (!selectedChannel?.models) return []
    return selectedChannel.models
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
  }, [selectedChannel])

  const saveMutation = useMutation({
    mutationFn: async () => {
      let jsonOutputParams: unknown
      try {
        jsonOutputParams = JSON.parse(jsonParamsText || '{}')
      } catch {
        throw new Error(t('JSON output params must be valid JSON'))
      }
      const result = await saveErrorInsightAISetting({
        ...form,
        sample_size: Number(form.sample_size) || 5,
        batch_limit: Number(form.batch_limit) || 10,
        json_output_params: jsonOutputParams,
      })
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to save AI settings'))
      }
      return result.data
    },
    onSuccess: (data) => {
      toast.success(t('AI settings saved'))
      queryClient.setQueryData(['error-insight', 'ai-settings'], data)
      props.onOpenChange(false)
    },
    onError: (error) => {
      toast.error(error.message || t('Failed to save AI settings'))
    },
  })

  const updateForm = (patch: Partial<ErrorInsightAISetting>) => {
    setForm((current) => ({ ...current, ...patch }))
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[88vh] overflow-y-auto sm:max-w-4xl'>
        <DialogHeader>
          <DialogTitle>{t('Error Insight AI Settings')}</DialogTitle>
          <DialogDescription>
            {t(
              'Configure the channel, model, prompt, and JSON output parameters used to generate candidate rules.'
            )}
          </DialogDescription>
        </DialogHeader>

        {settingQuery.isLoading ? (
          <div className='flex min-h-[260px] items-center justify-center'>
            <Loader2 className='text-muted-foreground size-6 animate-spin' />
          </div>
        ) : (
          <div className='grid gap-5 py-2'>
            <div className='flex items-center justify-between gap-4 rounded-xl border p-4'>
              <div>
                <p className='font-semibold'>{t('Enable AI Rule Generation')}</p>
                <p className='text-muted-foreground text-sm'>
                  {t('AI only generates candidate rules. Manual review is still required.')}
                </p>
              </div>
              <Switch
                checked={form.enabled}
                onCheckedChange={(value) => updateForm({ enabled: value })}
              />
            </div>

            <div className='grid gap-4 md:grid-cols-2'>
              <div className='space-y-2'>
                <label className='text-sm font-medium'>{t('AI Channel')}</label>
                <Select
                  value={form.channel_id > 0 ? String(form.channel_id) : ''}
                  onValueChange={(value) =>
                    updateForm({ channel_id: Number(value), model: '' })
                  }
                >
                  <SelectTrigger>
                    <SelectValue placeholder={t('Select channel')} />
                  </SelectTrigger>
                  <SelectContent>
                    {(channelsQuery.data ?? []).map((channel) => (
                      <SelectItem key={channel.id} value={String(channel.id)}>
                        #{channel.id} {channel.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>{t('AI Model')}</label>
                <Input
                  value={form.model}
                  list='error-insight-ai-models'
                  placeholder={t('Enter or select a model')}
                  onChange={(event) => updateForm({ model: event.target.value })}
                />
                <datalist id='error-insight-ai-models'>
                  {modelOptions.map((model) => (
                    <option key={model} value={model} />
                  ))}
                </datalist>
              </div>
            </div>

            <div className='grid gap-4 md:grid-cols-2'>
              <NumberField
                label={t('Sample Size')}
                value={form.sample_size}
                min={1}
                max={20}
                onChange={(value) => updateForm({ sample_size: value })}
              />
              <NumberField
                label={t('Batch Limit')}
                value={form.batch_limit}
                min={1}
                max={50}
                onChange={(value) => updateForm({ batch_limit: value })}
              />
            </div>

            <div className='grid gap-4 md:grid-cols-2'>
              <SwitchField
                label={t('Include Original Error')}
                description={t('Send original errors after optional redaction for better rule quality.')}
                checked={form.include_original_error}
                onChange={(value) => updateForm({ include_original_error: value })}
              />
              <SwitchField
                label={t('Redact Sensitive Text')}
                description={t('Mask keys, tokens, cookies, and secrets before sending samples to AI.')}
                checked={form.redact_sensitive}
                onChange={(value) => updateForm({ redact_sensitive: value })}
              />
            </div>

            <div className='space-y-2'>
              <label className='text-sm font-medium'>{t('Prompt Template')}</label>
              <Textarea
                value={form.prompt_template}
                className='min-h-64 font-mono text-xs'
                onChange={(event) =>
                  updateForm({ prompt_template: event.target.value })
                }
              />
              <p className='text-muted-foreground text-xs'>
                {t('Available variables: {{signature}}, {{sample_logs}}')}
              </p>
            </div>

            <div className='space-y-2'>
              <label className='text-sm font-medium'>{t('JSON Output Params')}</label>
              <Textarea
                value={jsonParamsText}
                className='min-h-36 font-mono text-xs'
                onChange={(event) => setJsonParamsText(event.target.value)}
              />
              <p className='text-muted-foreground text-xs'>
                {t('These params are merged into the AI chat request, such as response_format.')}
              </p>
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            disabled={saveMutation.isPending || settingQuery.isLoading}
            onClick={() => saveMutation.mutate()}
          >
            {saveMutation.isPending && <Loader2 className='size-4 animate-spin' />}
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function NumberField(props: {
  label: string
  value: number
  min: number
  max: number
  onChange: (value: number) => void
}) {
  return (
    <div className='space-y-2'>
      <label className='text-sm font-medium'>{props.label}</label>
      <Input
        type='number'
        min={props.min}
        max={props.max}
        value={props.value}
        onChange={(event) => props.onChange(Number(event.target.value))}
      />
    </div>
  )
}

function SwitchField(props: {
  label: string
  description: string
  checked: boolean
  onChange: (checked: boolean) => void
}) {
  return (
    <div className='flex items-center justify-between gap-4 rounded-xl border p-4'>
      <div>
        <p className='font-semibold'>{props.label}</p>
        <p className='text-muted-foreground text-sm'>{props.description}</p>
      </div>
      <Switch checked={props.checked} onCheckedChange={props.onChange} />
    </div>
  )
}
