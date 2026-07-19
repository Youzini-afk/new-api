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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Save, ShieldAlert } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { getChannels } from '@/features/channels/api'

import { getRiskControlSetting, saveRiskControlSetting } from '../api'
import type { RiskControlSetting } from '../types'

const DEFAULT_SETTING: RiskControlSetting = {
  enabled: false,
  schedule_enabled: false,
  interval_minutes: 15,
  window_hours: [1, 24],
  candidate_limit: 300,
  detail_limit: 20000,
  max_samples: 12,
  min_requests: 40,
  case_threshold: 40,
  high_rpm: 30,
  critical_rpm: 120,
  ip_fanout_threshold: 5,
  ua_fanout_threshold: 4,
  concurrency_threshold: 8,
  active_hours_threshold: 16,
  gateway_ua_markers: [
    'new-api',
    'newapi',
    'one-api',
    'oneapi',
    'sub2api',
    'axon',
  ],
  forbidden_client_ua_markers: [
    'lobster',
    'openclaw',
    'clawdia',
    'moltbot',
    'tavo/',
  ],
  case_cooldown_minutes: 360,
  include_request_content: true,
  redact_sensitive: true,
  agent_enabled: false,
  channel_id: 0,
  triage_model: '',
  judge_model: '',
  agent_min_rule_score: 40,
  max_agent_cases_per_run: 20,
  agent_concurrency: 4,
  agent_retry_count: 2,
  judge_min_final_score: 75,
  triage_prompt_template: '',
  judge_prompt_template: '',
  json_output_params: { response_format: { type: 'json_object' } },
  auto_action_enabled: false,
  auto_rate_limit_enabled: true,
  auto_freeze_token_enabled: true,
  auto_temp_block_enabled: true,
  auto_permanent_ban_enabled: false,
  auto_action_min_score: 82,
  auto_permanent_min_score: 95,
  auto_action_min_confidence: 0.9,
  rate_limit_per_minute: 10,
  temporary_block_minutes: 360,
  max_auto_actions_per_run: 10,
}

export function RiskSettingsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [form, setForm] = useState<RiskControlSetting>(DEFAULT_SETTING)
  const [windowsText, setWindowsText] = useState('1, 24')
  const [gatewayMarkersText, setGatewayMarkersText] = useState(
    DEFAULT_SETTING.gateway_ua_markers.join(', ')
  )
  const [forbiddenMarkersText, setForbiddenMarkersText] = useState(
    DEFAULT_SETTING.forbidden_client_ua_markers.join(', ')
  )
  const [jsonParamsText, setJsonParamsText] = useState(
    JSON.stringify(DEFAULT_SETTING.json_output_params, null, 2)
  )

  const settingQuery = useQuery({
    queryKey: ['risk-control', 'settings'],
    queryFn: async () => {
      const result = await getRiskControlSetting()
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load risk settings'))
      }
      return result.data
    },
  })

  const channelsQuery = useQuery({
    queryKey: ['channels', 'risk-control-agent'],
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
    setWindowsText(settingQuery.data.window_hours.join(', '))
    setGatewayMarkersText(settingQuery.data.gateway_ua_markers.join(', '))
    setForbiddenMarkersText(
      settingQuery.data.forbidden_client_ua_markers.join(', ')
    )
    setJsonParamsText(
      JSON.stringify(settingQuery.data.json_output_params ?? {}, null, 2)
    )
  }, [settingQuery.data])

  const selectedChannel = useMemo(
    () => channelsQuery.data?.find((channel) => channel.id === form.channel_id),
    [channelsQuery.data, form.channel_id]
  )
  const modelOptions = useMemo(() => {
    return (selectedChannel?.models ?? '')
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
  }, [selectedChannel])

  const saveMutation = useMutation({
    mutationFn: async () => {
      const windows = windowsText
        .split(',')
        .map((value) => Number(value.trim()))
        .filter((value) => Number.isInteger(value) && value > 0)
      if (windows.length === 0) {
        throw new Error(t('At least one valid screening window is required'))
      }
      let jsonOutputParams: unknown
      try {
        jsonOutputParams = JSON.parse(jsonParamsText || '{}')
      } catch {
        throw new Error(t('JSON output params must be valid JSON'))
      }
      if (
        jsonOutputParams === null ||
        typeof jsonOutputParams !== 'object' ||
        Array.isArray(jsonOutputParams)
      ) {
        throw new Error(t('JSON output params must be a JSON object'))
      }
      if (
        form.agent_enabled &&
        (form.channel_id <= 0 || !form.triage_model.trim())
      ) {
        throw new Error(t('Agent requires a channel and triage model'))
      }
      if (
        form.agent_enabled &&
        !form.triage_prompt_template.includes('{{case_evidence}}')
      ) {
        throw new Error(
          t('Triage prompt template must contain {{case_evidence}}')
        )
      }
      if (form.auto_action_enabled && !form.agent_enabled) {
        throw new Error(t('Automatic actions require Agent analysis'))
      }
      if (form.auto_permanent_ban_enabled && !form.judge_model.trim()) {
        throw new Error(t('Automatic permanent bans require a Judge model'))
      }
      if (
        form.judge_model.trim() &&
        !form.judge_prompt_template.includes('{{case_evidence}}')
      ) {
        throw new Error(
          t('Judge prompt template must contain {{case_evidence}}')
        )
      }
      const result = await saveRiskControlSetting({
        ...form,
        window_hours: windows,
        gateway_ua_markers: parseMarkerList(gatewayMarkersText),
        forbidden_client_ua_markers: parseMarkerList(forbiddenMarkersText),
        triage_model: form.triage_model.trim(),
        judge_model: form.judge_model.trim(),
        json_output_params: jsonOutputParams,
      })
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to save risk settings'))
      }
      return result.data
    },
    onSuccess: (data) => {
      toast.success(t('Risk settings saved'))
      queryClient.setQueryData(['risk-control', 'settings'], data)
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const updateForm = (patch: Partial<RiskControlSetting>) => {
    setForm((current) => ({ ...current, ...patch }))
  }

  if (settingQuery.isLoading) {
    return (
      <div className='flex min-h-60 items-center justify-center'>
        <Loader2 className='text-muted-foreground size-6 animate-spin' />
      </div>
    )
  }
  if (settingQuery.error) {
    return (
      <p className='text-destructive text-sm'>{settingQuery.error.message}</p>
    )
  }

  return (
    <div className='flex h-full min-h-0 flex-col gap-4'>
      <div className='flex items-center justify-between gap-3'>
        <div>
          <h2 className='font-semibold'>{t('Comprehensive risk control')}</h2>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Deterministic signals create cases first. Agent analysis and automatic actions are separately controlled.'
            )}
          </p>
        </div>
        <Button
          disabled={saveMutation.isPending}
          onClick={() => saveMutation.mutate()}
        >
          {saveMutation.isPending ? (
            <Loader2 className='size-4 animate-spin' />
          ) : (
            <Save className='size-4' />
          )}
          {t('Save changes')}
        </Button>
      </div>

      <div className='min-h-0 flex-1 overflow-auto pr-1'>
        <div className='mx-auto flex max-w-5xl flex-col gap-5 pb-4'>
          <SettingsSection
            title={t('Scanner')}
            description={t(
              'Reads existing admin logs in bounded windows. It does not add an LLM call to normal API requests.'
            )}
          >
            <div className='grid gap-3 md:grid-cols-2'>
              <SwitchField
                label={t('Enable risk control')}
                description={t(
                  'Allows manual deterministic screening and case creation.'
                )}
                checked={form.enabled}
                onChange={(enabled) => updateForm({ enabled })}
              />
              <SwitchField
                label={t('Enable scheduled screening')}
                description={t(
                  'Runs through the multi-node-safe system task scheduler.'
                )}
                checked={form.schedule_enabled}
                onChange={(schedule_enabled) =>
                  updateForm({ schedule_enabled })
                }
              />
            </div>
            <NumberGrid>
              <NumberField
                label={t('Interval minutes')}
                value={form.interval_minutes}
                onChange={(interval_minutes) =>
                  updateForm({ interval_minutes })
                }
              />
              <TextField
                label={t('Window hours')}
                value={windowsText}
                description={t('Comma separated, for example: 1, 24')}
                onChange={setWindowsText}
              />
              <NumberField
                label={t('Candidate limit')}
                value={form.candidate_limit}
                onChange={(candidate_limit) => updateForm({ candidate_limit })}
              />
              <NumberField
                label={t('Detail row limit')}
                value={form.detail_limit}
                onChange={(detail_limit) => updateForm({ detail_limit })}
              />
              <NumberField
                label={t('Minimum requests')}
                value={form.min_requests}
                onChange={(min_requests) => updateForm({ min_requests })}
              />
              <NumberField
                label={t('Case score threshold')}
                value={form.case_threshold}
                max={100}
                onChange={(case_threshold) => updateForm({ case_threshold })}
              />
              <NumberField
                label={t('Case cooldown minutes')}
                value={form.case_cooldown_minutes}
                onChange={(case_cooldown_minutes) =>
                  updateForm({ case_cooldown_minutes })
                }
              />
              <NumberField
                label={t('Evidence samples per case')}
                value={form.max_samples}
                onChange={(max_samples) => updateForm({ max_samples })}
              />
            </NumberGrid>
            <div className='grid gap-3 md:grid-cols-2'>
              <TextField
                label={t('Gateway UA markers')}
                value={gatewayMarkersText}
                description={t(
                  'Comma separated markers for redistribution gateways such as new-api, sub2api, and axon.'
                )}
                onChange={setGatewayMarkersText}
              />
              <TextField
                label={t('Forbidden client UA markers')}
                value={forbiddenMarkersText}
                description={t(
                  'Comma separated markers for paid or otherwise forbidden client software.'
                )}
                onChange={setForbiddenMarkersText}
              />
            </div>
          </SettingsSection>

          <SettingsSection
            title={t('Signal thresholds')}
            description={t(
              'Signals are combined; a single soft threshold does not automatically ban a user.'
            )}
          >
            <NumberGrid>
              <NumberField
                label={t('High RPM')}
                value={form.high_rpm}
                onChange={(high_rpm) => updateForm({ high_rpm })}
              />
              <NumberField
                label={t('Critical RPM')}
                value={form.critical_rpm}
                onChange={(critical_rpm) => updateForm({ critical_rpm })}
              />
              <NumberField
                label={t('IP fanout threshold')}
                value={form.ip_fanout_threshold}
                onChange={(ip_fanout_threshold) =>
                  updateForm({ ip_fanout_threshold })
                }
              />
              <NumberField
                label={t('UA fanout threshold')}
                value={form.ua_fanout_threshold}
                onChange={(ua_fanout_threshold) =>
                  updateForm({ ua_fanout_threshold })
                }
              />
              <NumberField
                label={t('Concurrency threshold')}
                value={form.concurrency_threshold}
                onChange={(concurrency_threshold) =>
                  updateForm({ concurrency_threshold })
                }
              />
              <NumberField
                label={t('Active hours threshold')}
                value={form.active_hours_threshold}
                onChange={(active_hours_threshold) =>
                  updateForm({ active_hours_threshold })
                }
              />
            </NumberGrid>
          </SettingsSection>

          <SettingsSection
            title={t('Evidence and content audit')}
            description={t(
              'Case evidence remains admin-only. Existing content audit behavior is preserved for abuse investigation.'
            )}
          >
            <div className='grid gap-3 md:grid-cols-2'>
              <SwitchField
                label={t('Include recorded request content')}
                description={t(
                  'Includes bounded request_params samples already captured by the existing log pipeline.'
                )}
                checked={form.include_request_content}
                onChange={(include_request_content) =>
                  updateForm({ include_request_content })
                }
              />
              <SwitchField
                label={t('Redact secrets before Agent calls')}
                description={t(
                  'Masks keys, tokens, cookies, and authorization values.'
                )}
                checked={form.redact_sensitive}
                onChange={(redact_sensitive) =>
                  updateForm({ redact_sensitive })
                }
              />
            </div>
          </SettingsSection>

          <SettingsSection
            title={t('Risk Agent')}
            description={t(
              'Uses a selected upstream channel directly for triage and optional independent judging.'
            )}
          >
            <SwitchField
              label={t('Enable Agent analysis')}
              description={t(
                'Only analyzes generated cases; normal relay requests never invoke this Agent.'
              )}
              checked={form.agent_enabled}
              onChange={(agent_enabled) => updateForm({ agent_enabled })}
            />
            <div className='grid gap-3 md:grid-cols-2'>
              <div className='space-y-1'>
                <Label>{t('Agent channel')}</Label>
                <Select
                  value={form.channel_id > 0 ? String(form.channel_id) : ''}
                  onValueChange={(value) =>
                    updateForm({
                      channel_id: Number(value),
                      triage_model: '',
                      judge_model: '',
                    })
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
              <ModelField
                id='risk-triage-models'
                label={t('Triage model')}
                value={form.triage_model}
                options={modelOptions}
                onChange={(triage_model) => updateForm({ triage_model })}
              />
              <ModelField
                id='risk-judge-models'
                label={t('Judge model (optional)')}
                value={form.judge_model}
                options={modelOptions}
                onChange={(judge_model) => updateForm({ judge_model })}
              />
              <NumberField
                label={t('Agent minimum rule score')}
                value={form.agent_min_rule_score}
                max={100}
                onChange={(agent_min_rule_score) =>
                  updateForm({ agent_min_rule_score })
                }
              />
              <NumberField
                label={t('Maximum Agent cases per run')}
                value={form.max_agent_cases_per_run}
                onChange={(max_agent_cases_per_run) =>
                  updateForm({ max_agent_cases_per_run })
                }
              />
              <NumberField
                label={t('Concurrent Agent cases')}
                value={form.agent_concurrency}
                max={16}
                onChange={(agent_concurrency) =>
                  updateForm({ agent_concurrency })
                }
              />
              <NumberField
                label={t('Agent retry count')}
                value={form.agent_retry_count}
                max={5}
                onChange={(agent_retry_count) =>
                  updateForm({ agent_retry_count })
                }
              />
              <NumberField
                label={t('Judge minimum final score')}
                value={form.judge_min_final_score}
                max={100}
                onChange={(judge_min_final_score) =>
                  updateForm({ judge_min_final_score })
                }
              />
            </div>
            <PromptField
              label={t('Triage prompt template')}
              value={form.triage_prompt_template}
              onChange={(triage_prompt_template) =>
                updateForm({ triage_prompt_template })
              }
            />
            <PromptField
              label={t('Judge prompt template')}
              value={form.judge_prompt_template}
              onChange={(judge_prompt_template) =>
                updateForm({ judge_prompt_template })
              }
            />
            <div className='space-y-1'>
              <Label>{t('JSON output params')}</Label>
              <Textarea
                value={jsonParamsText}
                onChange={(event) => setJsonParamsText(event.target.value)}
                className='min-h-32 font-mono text-xs'
                spellCheck={false}
              />
            </div>
          </SettingsSection>

          <SettingsSection
            title={t('Automatic actions')}
            description={t(
              'Disabled by default. Requires Agent confidence and per-action switches; permanent bans also require Judge agreement and repeat evidence.'
            )}
            danger
          >
            <div className='flex items-center gap-2 rounded-lg border border-orange-500/30 bg-orange-500/10 p-3'>
              <ShieldAlert className='size-4 text-orange-600 dark:text-orange-400' />
              <p className='text-xs'>
                {t(
                  'Hard UA and prompt red-line rules keep their existing immediate local auto-ban behavior and are independent of this matrix.'
                )}
              </p>
            </div>
            <SwitchField
              label={t('Enable automatic actions')}
              description={t(
                'Master switch for Agent-recommended enforcement.'
              )}
              checked={form.auto_action_enabled}
              onChange={(auto_action_enabled) =>
                updateForm({ auto_action_enabled })
              }
            />
            <div className='grid gap-3 md:grid-cols-2'>
              <SwitchField
                label={t('Allow automatic rate limit')}
                description={t('Applies a temporary per-user RPM cap.')}
                checked={form.auto_rate_limit_enabled}
                onChange={(auto_rate_limit_enabled) =>
                  updateForm({ auto_rate_limit_enabled })
                }
              />
              <SwitchField
                label={t('Allow automatic token freeze')}
                description={t(
                  'Disables only the token implicated by the case.'
                )}
                checked={form.auto_freeze_token_enabled}
                onChange={(auto_freeze_token_enabled) =>
                  updateForm({ auto_freeze_token_enabled })
                }
              />
              <SwitchField
                label={t('Allow automatic temporary block')}
                description={t(
                  'Temporarily blocks the user through cached risk state.'
                )}
                checked={form.auto_temp_block_enabled}
                onChange={(auto_temp_block_enabled) =>
                  updateForm({ auto_temp_block_enabled })
                }
              />
              <SwitchField
                label={t('Allow automatic permanent ban')}
                description={t(
                  'Highest impact. Requires a Judge result, a very high score, and repeated cases.'
                )}
                checked={form.auto_permanent_ban_enabled}
                onChange={(auto_permanent_ban_enabled) =>
                  updateForm({ auto_permanent_ban_enabled })
                }
                danger
              />
            </div>
            <NumberGrid>
              <NumberField
                label={t('Minimum auto-action score')}
                value={form.auto_action_min_score}
                max={100}
                onChange={(auto_action_min_score) =>
                  updateForm({ auto_action_min_score })
                }
              />
              <NumberField
                label={t('Minimum permanent-ban score')}
                value={form.auto_permanent_min_score}
                max={100}
                onChange={(auto_permanent_min_score) =>
                  updateForm({ auto_permanent_min_score })
                }
              />
              <NumberField
                label={t('Minimum Agent confidence')}
                value={form.auto_action_min_confidence}
                min={0}
                max={1}
                step={0.01}
                onChange={(auto_action_min_confidence) =>
                  updateForm({ auto_action_min_confidence })
                }
              />
              <NumberField
                label={t('Rate limit per minute')}
                value={form.rate_limit_per_minute}
                onChange={(rate_limit_per_minute) =>
                  updateForm({ rate_limit_per_minute })
                }
              />
              <NumberField
                label={t('Temporary block minutes')}
                value={form.temporary_block_minutes}
                onChange={(temporary_block_minutes) =>
                  updateForm({ temporary_block_minutes })
                }
              />
              <NumberField
                label={t('Maximum automatic actions per run')}
                value={form.max_auto_actions_per_run}
                onChange={(max_auto_actions_per_run) =>
                  updateForm({ max_auto_actions_per_run })
                }
              />
            </NumberGrid>
          </SettingsSection>
        </div>
      </div>
    </div>
  )
}

function SettingsSection(props: {
  title: string
  description: string
  children: React.ReactNode
  danger?: boolean
}) {
  const { t } = useTranslation()
  return (
    <section
      className={
        props.danger
          ? 'rounded-xl border border-orange-500/30 bg-orange-500/5 p-4'
          : 'bg-card rounded-xl border p-4'
      }
    >
      <div className='mb-4 flex items-start justify-between gap-3'>
        <div>
          <h3 className='font-semibold'>{props.title}</h3>
          <p className='text-muted-foreground text-xs'>{props.description}</p>
        </div>
        {props.danger && <Badge variant='outline'>{t('Guarded')}</Badge>}
      </div>
      <div className='flex flex-col gap-4'>{props.children}</div>
    </section>
  )
}

function NumberGrid(props: { children: React.ReactNode }) {
  return (
    <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
      {props.children}
    </div>
  )
}

function NumberField(props: {
  label: string
  value: number
  onChange: (value: number) => void
  min?: number
  max?: number
  step?: number
}) {
  return (
    <div className='space-y-1'>
      <Label>{props.label}</Label>
      <Input
        type='number'
        min={props.min ?? 1}
        max={props.max}
        step={props.step}
        value={props.value}
        onChange={(event) => props.onChange(Number(event.target.value))}
      />
    </div>
  )
}

function TextField(props: {
  label: string
  value: string
  description?: string
  onChange: (value: string) => void
}) {
  return (
    <div className='space-y-1'>
      <Label>{props.label}</Label>
      <Input
        value={props.value}
        onChange={(event) => props.onChange(event.target.value)}
      />
      {props.description && (
        <p className='text-muted-foreground text-xs'>{props.description}</p>
      )}
    </div>
  )
}

function ModelField(props: {
  id: string
  label: string
  value: string
  options: string[]
  onChange: (value: string) => void
}) {
  return (
    <div className='space-y-1'>
      <Label>{props.label}</Label>
      <Input
        value={props.value}
        list={props.id}
        onChange={(event) => props.onChange(event.target.value)}
      />
      <datalist id={props.id}>
        {props.options.map((option) => (
          <option key={option} value={option} />
        ))}
      </datalist>
    </div>
  )
}

function PromptField(props: {
  label: string
  value: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='space-y-1'>
      <Label>{props.label}</Label>
      <Textarea
        value={props.value}
        onChange={(event) => props.onChange(event.target.value)}
        className='min-h-56 font-mono text-xs'
      />
      <p className='text-muted-foreground text-xs'>
        {t('Available variable: case_evidence placeholder')}
      </p>
    </div>
  )
}

function SwitchField(props: {
  label: string
  description: string
  checked: boolean
  onChange: (value: boolean) => void
  danger?: boolean
}) {
  return (
    <div
      className={
        props.danger
          ? 'border-destructive/30 bg-destructive/5 flex items-center justify-between gap-4 rounded-xl border p-4'
          : 'flex items-center justify-between gap-4 rounded-xl border p-4'
      }
    >
      <div>
        <p className='font-medium'>{props.label}</p>
        <p className='text-muted-foreground text-xs'>{props.description}</p>
      </div>
      <Switch checked={props.checked} onCheckedChange={props.onChange} />
    </div>
  )
}

function parseMarkerList(raw: string): string[] {
  return [
    ...new Set(
      raw
        .split(',')
        .map((value) => value.trim())
        .filter(Boolean)
    ),
  ]
}
