import { ArrowDown, ArrowUp, Loader2, RefreshCw, Settings, Sparkles, Trash2 } from 'lucide-react'
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
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
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
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { getChannels } from '@/features/channels/api'

import { SettingsSwitchField } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  DEFAULT_RULE_ENABLED,
  RELAY_ERROR_DEFAULT_MESSAGES,
  RELAY_ERROR_GOVERNANCE_OPTION_KEY,
  RELAY_ERROR_RULE_HINTS,
  RELAY_ERROR_RULE_LABELS,
  buildGovernanceRows,
  serializeGovernanceConfig,
} from './constants'
import type {
  ErrorGovernanceAIOrganizeResult,
  ErrorGovernanceAISetting,
  GovernanceRuleRow,
  RelayErrorCustomRule,
  RelayErrorGovernanceConfig,
} from './types'
import {
  generateErrorGovernanceAIOrganization,
  getErrorGovernanceAISetting,
  saveErrorGovernanceAISetting,
} from './api'

type ErrorGovernanceSectionProps = {
  /** Raw JSON string of the `relay_error_governance` system option. */
  defaultValue: string
}

const defaultAISetting: ErrorGovernanceAISetting = {
  enabled: false,
  channel_id: 0,
  model: '',
  redact_sensitive: true,
  prompt_template: '',
  json_output_params: {
    response_format: {
      type: 'json_object',
    },
  },
}

function parseGovernanceConfig(raw: string): RelayErrorGovernanceConfig | null {
  const text = raw?.trim()
  if (!text) return null
  try {
    const parsed = JSON.parse(text) as Partial<RelayErrorGovernanceConfig>
    if (typeof parsed !== 'object' || parsed === null) return null
    return {
      enabled: typeof parsed.enabled === 'boolean' ? parsed.enabled : false,
      rules:
        parsed.rules && typeof parsed.rules === 'object' ? parsed.rules : {},
      custom_rules: Array.isArray(parsed.custom_rules)
        ? parsed.custom_rules
        : [],
    }
  } catch {
    return null
  }
}

function normalizePattern(rule: RelayErrorCustomRule) {
  return (rule.match_pattern || '').trim().toLowerCase()
}

function getCustomRuleConflicts(
  rule: RelayErrorCustomRule,
  index: number,
  rules: RelayErrorCustomRule[]
) {
  const conflicts: string[] = []
  const pattern = normalizePattern(rule)
  for (let i = 0; i < rules.length; i += 1) {
    if (i === index) continue
    const other = rules[i]
    const otherPattern = normalizePattern(other)
    if (!pattern || !otherPattern) continue
    if (rule.rule_code && rule.rule_code === other.rule_code) {
      conflicts.push(`duplicate code: ${other.rule_code}`)
      continue
    }
    if (rule.match_type === other.match_type && pattern === otherPattern) {
      conflicts.push(`same pattern: ${other.rule_code}`)
      continue
    }
    if (rule.match_type === 'contains' && other.match_type === 'contains') {
      if (pattern.includes(otherPattern) || otherPattern.includes(pattern)) {
        conflicts.push(`overlap: ${other.rule_code}`)
      }
      continue
    }
    if (rule.match_type === 'regex') {
      try {
        if (new RegExp(rule.match_pattern).test(otherPattern)) {
          conflicts.push(`regex overlaps: ${other.rule_code}`)
        }
      } catch {
        conflicts.push('invalid regex')
      }
    }
    if (other.match_type === 'regex') {
      try {
        if (new RegExp(other.match_pattern).test(pattern)) {
          conflicts.push(`covered by: ${other.rule_code}`)
        }
      } catch {
        conflicts.push(`other invalid regex: ${other.rule_code}`)
      }
    }
  }
  return Array.from(new Set(conflicts))
}

export function ErrorGovernanceSection({
  defaultValue,
}: ErrorGovernanceSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()

  const parsed = useMemo(
    () => parseGovernanceConfig(defaultValue),
    [defaultValue]
  )
  const parsedRows = useMemo(() => buildGovernanceRows(parsed, t), [parsed, t])

  // Draft state — null until the admin edits something, so server values stay
  // authoritative until a change is made (matches the api-info section pattern).
  const [draftEnabled, setDraftEnabled] = useState<boolean | null>(null)
  const [draftRows, setDraftRows] = useState<GovernanceRuleRow[] | null>(null)
  const [draftCustomRules, setDraftCustomRules] = useState<RelayErrorCustomRule[] | null>(null)
  const [aiSettingsOpen, setAISettingsOpen] = useState(false)
  const [aiResultOpen, setAIResultOpen] = useState(false)
  const [aiResult, setAIResult] = useState<ErrorGovernanceAIOrganizeResult | null>(null)
  const [aiForm, setAIForm] = useState<ErrorGovernanceAISetting>(defaultAISetting)
  const [jsonParamsText, setJsonParamsText] = useState(JSON.stringify(defaultAISetting.json_output_params, null, 2))

  // Reset drafts whenever the underlying option is refreshed after a save.
  useEffect(() => {
    setDraftEnabled(null)
    setDraftRows(null)
    setDraftCustomRules(null)
  }, [defaultValue])

  const effectiveEnabled = draftEnabled ?? parsed?.enabled ?? false
  const effectiveRows = draftRows ?? parsedRows
  const customRules = draftCustomRules ?? parsed?.custom_rules ?? []
  const customRuleConflicts = useMemo(
    () => customRules.map((rule, index) => getCustomRuleConflicts(rule, index, customRules)),
    [customRules]
  )
  const hasChanges = draftEnabled !== null || draftRows !== null || draftCustomRules !== null

  const aiSettingQuery = useQuery({
    queryKey: ['error-governance-ai', 'settings'],
    enabled: aiSettingsOpen,
    queryFn: async () => {
      const result = await getErrorGovernanceAISetting()
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load AI governance settings'))
      }
      return result.data
    },
  })

  const channelsQuery = useQuery({
    queryKey: ['channels', 'error-governance-ai'],
    enabled: aiSettingsOpen,
    queryFn: async () => {
      const result = await getChannels({ page_size: 100, status: 'enabled' })
      if (!result.success) {
        throw new Error(result.message || t('Failed to load channels'))
      }
      return result.data?.items ?? []
    },
  })

  useEffect(() => {
    if (!aiSettingQuery.data) return
    setAIForm(aiSettingQuery.data)
    setJsonParamsText(JSON.stringify(aiSettingQuery.data.json_output_params, null, 2))
  }, [aiSettingQuery.data])

  const selectedAIChannel = useMemo(
    () => channelsQuery.data?.find((channel) => channel.id === aiForm.channel_id),
    [channelsQuery.data, aiForm.channel_id]
  )

  const aiModelOptions = useMemo(() => {
    if (!selectedAIChannel?.models) return []
    return selectedAIChannel.models
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
  }, [selectedAIChannel])

  const saveAISettingMutation = useMutation({
    mutationFn: async () => {
      let jsonOutputParams: unknown
      try {
        jsonOutputParams = JSON.parse(jsonParamsText || '{}')
      } catch {
        throw new Error(t('JSON output params must be valid JSON'))
      }
      const result = await saveErrorGovernanceAISetting({
        ...aiForm,
        json_output_params: jsonOutputParams,
      })
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to save AI governance settings'))
      }
      return result.data
    },
    onSuccess: (data) => {
      toast.success(t('AI governance settings saved'))
      queryClient.setQueryData(['error-governance-ai', 'settings'], data)
      setAISettingsOpen(false)
    },
    onError: (error) => {
      toast.error(error.message || t('Failed to save AI governance settings'))
    },
  })

  const organizeAIMutation = useMutation({
    mutationFn: async () => {
      const result = await generateErrorGovernanceAIOrganization(
        serializeGovernanceConfig(effectiveEnabled, effectiveRows, t, customRules)
      )
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to organize custom rules'))
      }
      return result.data
    },
    onSuccess: (data) => {
      setAIResult(data)
      setAIResultOpen(true)
      toast.success(t('AI governance organization generated'))
    },
    onError: (error) => {
      toast.error(error.message || t('Failed to organize custom rules'))
    },
  })

  const updateRow = (code: string, patch: Partial<GovernanceRuleRow>) => {
    setDraftRows((draft) =>
      (draft ?? effectiveRows.map((row) => ({ ...row }))).map((row) =>
        row.code === code ? { ...row, ...patch } : row
      )
    )
  }

  const resetRule = (code: GovernanceRuleRow['code']) => {
    const defaultMessageKey = RELAY_ERROR_DEFAULT_MESSAGES[code] ?? ''
    updateRow(code, {
      enabled: DEFAULT_RULE_ENABLED,
      message: t(defaultMessageKey),
    })
  }

  const updateCustomRule = (index: number, patch: Partial<RelayErrorCustomRule>) => {
    setDraftCustomRules((draft) =>
      (draft ?? customRules.map((rule) => ({ ...rule }))).map((rule, ruleIndex) =>
        ruleIndex === index ? { ...rule, ...patch } : rule
      )
    )
  }

  const moveCustomRule = (index: number, direction: -1 | 1) => {
    setDraftCustomRules((draft) => {
      const next = draft ?? customRules.map((rule) => ({ ...rule }))
      const target = index + direction
      if (target < 0 || target >= next.length) return next
      const copy = [...next]
      const current = copy[index]
      copy[index] = copy[target]
      copy[target] = current
      return copy
    })
  }

  const deleteCustomRule = (index: number) => {
    setDraftCustomRules((draft) =>
      (draft ?? customRules.map((rule) => ({ ...rule }))).filter((_, ruleIndex) => ruleIndex !== index)
    )
  }

  const handleReset = () => {
    setDraftEnabled(null)
    setDraftRows(null)
    setDraftCustomRules(null)
  }

  const handleSave = async () => {
    const config = serializeGovernanceConfig(effectiveEnabled, effectiveRows, t, customRules)
    await updateOption.mutateAsync({
      key: RELAY_ERROR_GOVERNANCE_OPTION_KEY,
      value: JSON.stringify(config),
    })
  }

  const updateAIForm = (patch: Partial<ErrorGovernanceAISetting>) => {
    setAIForm((current) => ({ ...current, ...patch }))
  }

  const applyAIResult = () => {
    if (!aiResult) return
    setDraftCustomRules(aiResult.rules.map((rule) => ({ ...rule })))
    setAIResultOpen(false)
    toast.success(t('AI organization applied to draft. Review and save to publish.'))
  }

  return (
    <SettingsSection title={t('Relay Error Governance')}>
      <SettingsSwitchField
        checked={effectiveEnabled}
        onCheckedChange={(checked) => setDraftEnabled(checked)}
        label={t('Enable error governance')}
        description={t(
          'When enabled, governed relay errors are replaced with the configured messages before reaching clients.'
        )}
      />

      <div className='text-muted-foreground text-xs'>
        {t(
          'Rule codes are fixed by the backend. Each rule message is editable; use reset to restore the default response message.'
        )}
      </div>

      <div className='overflow-x-auto rounded-lg border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='min-w-[160px]'>{t('Rule Code')}</TableHead>
              <TableHead className='min-w-[220px]'>
                {t('Default Message')}
              </TableHead>
              <TableHead className='w-[90px] text-center'>
                {t('Enabled')}
              </TableHead>
              <TableHead className='min-w-[260px]'>
                {t('Custom Message')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {effectiveRows.map((row) => {
              const defaultMessage =
                RELAY_ERROR_DEFAULT_MESSAGES[row.code] ?? ''
              const translatedDefaultMessage = t(defaultMessage)
              const label = RELAY_ERROR_RULE_LABELS[row.code] ?? row.code
              const hint = RELAY_ERROR_RULE_HINTS[row.code] ?? ''
              return (
                <TableRow key={row.code}>
                  <TableCell>
                    <div className='flex flex-col gap-1'>
                      <Badge variant='outline' className='w-fit font-mono'>
                        {row.code}
                      </Badge>
                      <span className='text-sm font-medium'>{t(label)}</span>
                      {hint && (
                        <span className='text-muted-foreground text-xs'>
                          {t(hint)}
                        </span>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className='text-muted-foreground text-sm'>
                    <span
                      className='line-clamp-2 max-w-[280px]'
                      title={translatedDefaultMessage}
                    >
                      {translatedDefaultMessage}
                    </span>
                  </TableCell>
                  <TableCell className='text-center'>
                    <Switch
                      checked={row.enabled}
                      onCheckedChange={(checked) =>
                        updateRow(row.code, { enabled: checked })
                      }
                      aria-label={t('Toggle governance for {{code}}', {
                        code: row.code,
                      })}
                    />
                  </TableCell>
                  <TableCell>
                    <div className='flex items-start gap-2'>
                      <Textarea
                        value={row.message}
                        onChange={(e) =>
                          updateRow(row.code, { message: e.target.value })
                        }
                        placeholder={translatedDefaultMessage}
                        rows={2}
                        aria-label={t('Custom message for {{code}}', {
                          code: row.code,
                        })}
                      />
                      <Button
                        type='button'
                        variant='ghost'
                        size='icon'
                        onClick={() => resetRule(row.code)}
                        aria-label={t('Reset rule {{code}} to default', {
                          code: row.code,
                        })}
                        title={t('Reset to default')}
                      >
                        <RefreshCw className='h-4 w-4' />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>

      <div className='space-y-3'>
        <div className='flex items-center justify-between gap-3'>
          <div>
            <h3 className='text-sm font-semibold'>{t('Custom AI Rules')}</h3>
            <p className='text-muted-foreground text-xs'>
              {t('Rules approved from Error Insight AI generation are shown here.')}
            </p>
            <p className='text-muted-foreground text-xs'>
              {t('Custom rules are matched from top to bottom. The first matching enabled rule wins.')}
            </p>
          </div>
          <div className='flex items-center gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setAISettingsOpen(true)}
            >
              <Settings className='h-4 w-4' />
              {t('AI Governance Settings')}
            </Button>
            <Button
              type='button'
              size='sm'
              disabled={organizeAIMutation.isPending || customRules.length === 0}
              onClick={() => organizeAIMutation.mutate()}
            >
              {organizeAIMutation.isPending ? (
                <Loader2 className='h-4 w-4 animate-spin' />
              ) : (
                <Sparkles className='h-4 w-4' />
              )}
              {t('AI Organize')}
            </Button>
            <Badge variant='secondary'>{customRules.length}</Badge>
          </div>
        </div>
        <div className='overflow-x-auto rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='min-w-[180px]'>{t('Rule Code')}</TableHead>
                <TableHead className='min-w-[170px]'>{t('Match Type')}</TableHead>
                <TableHead className='min-w-[260px]'>{t('Match Pattern')}</TableHead>
                <TableHead className='min-w-[260px]'>{t('Safe Error Message')}</TableHead>
                <TableHead className='w-[90px] text-center'>{t('Enabled')}</TableHead>
                <TableHead className='min-w-[220px]'>{t('Conflict')}</TableHead>
                <TableHead className='w-[150px] text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {customRules.length ? (
                customRules.map((rule, index) => (
                  <TableRow key={`${rule.rule_code}-${index}`}>
                    <TableCell>
                      <div className='flex flex-col gap-1'>
                        <Badge variant='outline' className='w-fit font-mono'>
                          #{index + 1} ·{' '}
                          {rule.rule_code}
                        </Badge>
                        {rule.category ? (
                          <span className='text-muted-foreground text-xs'>
                            {rule.category}
                          </span>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant='secondary'>{rule.match_type || '-'}</Badge>
                    </TableCell>
                    <TableCell>
                      <code className='text-muted-foreground line-clamp-2 max-w-[340px] break-all text-xs'>
                        {rule.match_pattern || '-'}
                      </code>
                    </TableCell>
                    <TableCell>
                      <span className='line-clamp-2 max-w-[360px] text-sm'>
                        {rule.safe_error_message || '-'}
                      </span>
                    </TableCell>
                    <TableCell className='text-center'>
                      <Switch
                        checked={rule.enabled}
                        onCheckedChange={(checked) => updateCustomRule(index, { enabled: checked })}
                        aria-label={t('Toggle custom rule {{code}}', { code: rule.rule_code })}
                      />
                    </TableCell>
                    <TableCell>
                      {customRuleConflicts[index]?.length ? (
                        <div className='flex flex-col gap-1'>
                          <Badge className='w-fit bg-destructive/15 text-destructive hover:bg-destructive/15'>
                            {t('Conflict detected')}
                          </Badge>
                          <span className='text-muted-foreground text-xs'>
                            {customRuleConflicts[index].join(', ')}
                          </span>
                        </div>
                      ) : (
                        <span className='text-muted-foreground text-sm'>-</span>
                      )}
                    </TableCell>
                    <TableCell className='text-right'>
                      <div className='flex justify-end gap-1'>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          disabled={index === 0}
                          onClick={() => moveCustomRule(index, -1)}
                          aria-label={t('Move up')}
                        >
                          <ArrowUp className='h-4 w-4' />
                        </Button>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          disabled={index === customRules.length - 1}
                          onClick={() => moveCustomRule(index, 1)}
                          aria-label={t('Move down')}
                        >
                          <ArrowDown className='h-4 w-4' />
                        </Button>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          className='text-destructive hover:text-destructive'
                          onClick={() => deleteCustomRule(index)}
                          aria-label={t('Delete custom rule')}
                        >
                          <Trash2 className='h-4 w-4' />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={7} className='text-muted-foreground py-8 text-center text-sm'>
                    {t('No custom AI rules saved yet.')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      </div>

      <SettingsPageFormActions
        onSave={handleSave}
        onReset={handleReset}
        isSaving={updateOption.isPending}
        isSaveDisabled={!hasChanges}
        isResetDisabled={!hasChanges}
        saveLabel='Save error governance'
      />

      <Dialog open={aiSettingsOpen} onOpenChange={setAISettingsOpen}>
        <DialogContent className='max-h-[88vh] overflow-y-auto sm:max-w-4xl'>
          <DialogHeader>
            <DialogTitle>{t('AI Governance Settings')}</DialogTitle>
            <DialogDescription>
              {t('Configure the channel, model, prompt, and JSON output parameters used to organize custom governance rules.')}
            </DialogDescription>
          </DialogHeader>

          {aiSettingQuery.isLoading ? (
            <div className='flex min-h-[240px] items-center justify-center'>
              <Loader2 className='text-muted-foreground size-6 animate-spin' />
            </div>
          ) : (
            <div className='grid gap-5 py-2'>
              <div className='flex items-center justify-between gap-4 rounded-xl border p-4'>
                <div>
                  <p className='font-semibold'>{t('Enable AI Governance')}</p>
                  <p className='text-muted-foreground text-sm'>
                    {t('AI only generates an organization draft. Manual review and save are still required.')}
                  </p>
                </div>
                <Switch
                  checked={aiForm.enabled}
                  onCheckedChange={(value) => updateAIForm({ enabled: value })}
                />
              </div>

              <div className='grid gap-4 md:grid-cols-2'>
                <div className='space-y-2'>
                  <label className='text-sm font-medium'>{t('AI Channel')}</label>
                  <Select
                    value={aiForm.channel_id > 0 ? String(aiForm.channel_id) : ''}
                    onValueChange={(value) => updateAIForm({ channel_id: Number(value), model: '' })}
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
                    value={aiForm.model}
                    list='error-governance-ai-models'
                    placeholder={t('Enter or select a model')}
                    onChange={(event) => updateAIForm({ model: event.target.value })}
                  />
                  <datalist id='error-governance-ai-models'>
                    {aiModelOptions.map((model) => (
                      <option key={model} value={model} />
                    ))}
                  </datalist>
                </div>
              </div>

              <div className='flex items-center justify-between gap-4 rounded-xl border p-4'>
                <div>
                  <p className='font-semibold'>{t('Redact Sensitive Text')}</p>
                  <p className='text-muted-foreground text-sm'>
                    {t('Mask keys, tokens, cookies, and secrets before sending rules to AI.')}
                  </p>
                </div>
                <Switch
                  checked={aiForm.redact_sensitive}
                  onCheckedChange={(value) => updateAIForm({ redact_sensitive: value })}
                />
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>{t('Prompt Template')}</label>
                <Textarea
                  value={aiForm.prompt_template}
                  className='min-h-64 font-mono text-xs'
                  onChange={(event) => updateAIForm({ prompt_template: event.target.value })}
                />
                <p className='text-muted-foreground text-xs'>
                  {t('Available variables: {{governance_config}}, {{conflicts}}')}
                </p>
              </div>

              <div className='space-y-2'>
                <label className='text-sm font-medium'>{t('JSON Output Params')}</label>
                <Textarea
                  value={jsonParamsText}
                  className='min-h-32 font-mono text-xs'
                  onChange={(event) => setJsonParamsText(event.target.value)}
                />
              </div>
            </div>
          )}

          <DialogFooter>
            <Button variant='outline' onClick={() => setAISettingsOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={saveAISettingMutation.isPending || aiSettingQuery.isLoading}
              onClick={() => saveAISettingMutation.mutate()}
            >
              {saveAISettingMutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
              {t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Sheet open={aiResultOpen} onOpenChange={setAIResultOpen}>
        <SheetContent className='w-[92vw] p-0 sm:max-w-4xl' side='right'>
          <SheetHeader className='border-border/70 border-b px-6 py-5'>
            <SheetTitle className='text-2xl font-bold'>{t('AI Governance Organization')}</SheetTitle>
            <SheetDescription>
              {t('Review the organized draft, then apply it to the current custom rules draft before saving.')}
            </SheetDescription>
          </SheetHeader>
          <div className='flex-1 space-y-5 overflow-y-auto px-6 py-5'>
            {aiResult?.summary ? (
              <div className='bg-muted/40 rounded-xl p-4 text-sm whitespace-pre-wrap'>
                {aiResult.summary}
              </div>
            ) : null}
            <div className='overflow-x-auto rounded-lg border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className='min-w-[180px]'>{t('Rule Code')}</TableHead>
                    <TableHead className='min-w-[140px]'>{t('Match Type')}</TableHead>
                    <TableHead className='min-w-[260px]'>{t('Match Pattern')}</TableHead>
                    <TableHead className='min-w-[260px]'>{t('Safe Error Message')}</TableHead>
                    <TableHead className='w-[90px]'>{t('Enabled')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {aiResult?.rules.length ? (
                    aiResult.rules.map((rule, index) => (
                      <TableRow key={`${rule.rule_code}-${index}`}>
                        <TableCell>
                          <Badge variant='outline' className='font-mono'>#{index + 1} · {rule.rule_code}</Badge>
                        </TableCell>
                        <TableCell>
                          <Badge variant='secondary'>{rule.match_type || '-'}</Badge>
                        </TableCell>
                        <TableCell>
                          <code className='text-muted-foreground line-clamp-2 max-w-[360px] break-all text-xs'>
                            {rule.match_pattern || '-'}
                          </code>
                        </TableCell>
                        <TableCell>
                          <span className='line-clamp-2 max-w-[360px] text-sm'>
                            {rule.safe_error_message || '-'}
                          </span>
                        </TableCell>
                        <TableCell>{rule.enabled ? t('Yes') : t('No')}</TableCell>
                      </TableRow>
                    ))
                  ) : (
                    <TableRow>
                      <TableCell colSpan={5} className='text-muted-foreground py-8 text-center text-sm'>
                        {t('No custom AI rules saved yet.')}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </div>
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={() => setAIResultOpen(false)}>
                {t('Cancel')}
              </Button>
              <Button disabled={!aiResult?.rules.length} onClick={applyAIResult}>
                {t('Apply to Draft')}
              </Button>
            </div>
          </div>
        </SheetContent>
      </Sheet>
    </SettingsSection>
  )
}
