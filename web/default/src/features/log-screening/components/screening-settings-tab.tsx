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
 * Settings tab for log screening. Edits the option keys exposed by the backend
 * option API. JSON-stored options (regex rule arrays, log_screening,
 * relay_param_record) are edited through validating textareas; complex JSON is
 * never expanded into the page layout.
 *
 * NOTE: CheckSensitiveAutoBanSyncEnabled / AutoBanSync / ban_sync.* are
 * intentionally not editable here — ban_sync is deprecated for this branch.
 */
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2, Save } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import { useSystemOptions } from '@/features/system-settings/hooks/use-system-options'
import { useUpdateOption } from '@/features/system-settings/hooks/use-update-option'
import {
  BOOLEAN_OPTION_KEYS,
  isJsonOptionKey,
} from '../constants'
import type {
  LogScreeningOptionKey,
  LogScreeningOptionValues,
} from '../types'
import { validateJson } from '../lib/utils'

// Default option values (used when an option is missing from the API response).
const DEFAULT_OPTION_VALUES: LogScreeningOptionValues = {
  CheckSensitiveEnabled: false,
  CheckSensitiveOnPromptEnabled: false,
  SensitiveWords: '',
  SensitivePromptRegexRules: '[]',
  SensitivePromptBlockedMessage: '',
  CheckSensitiveOnUAEnabled: false,
  SensitiveUABlockedRegexes: '',
  SensitiveUARegexRules: '[]',
  SensitiveUAGroupRegexRules: '{}',
  SensitiveUABlockedMessage: '',
  CheckSensitiveOnEmptyUAEnabled: false,
  CheckSensitiveOnEmptyUAAutoBanEnabled: false,
  SensitiveEmptyUABlockedMessage: '',
  SensitiveEmptyUABlockedHTTPStatusCode: '400',
  SensitiveEmptyUABlockedErrorCode: '',
  log_screening: '{"enabled":true,"rules":[],"expire_days":7}',
  relay_param_record: '{}',
}

/** String keys whose stored value should be edited as a number. */
const NUMBER_OPTION_KEYS: ReadonlySet<LogScreeningOptionKey> = new Set([
  'SensitiveEmptyUABlockedHTTPStatusCode',
])

function isBooleanKey(key: LogScreeningOptionKey): boolean {
  return (BOOLEAN_OPTION_KEYS as readonly string[]).includes(key)
}

function parseStoredValue(key: LogScreeningOptionKey, raw: string): unknown {
  if (isBooleanKey(key)) return raw === 'true' || raw === '1'
  return raw
}

function deriveOptionValues(
  options: Array<{ key: string; value: string }> | undefined
): LogScreeningOptionValues {
  const result = { ...DEFAULT_OPTION_VALUES }
  if (!options) return result
  for (const opt of options) {
    const key = opt.key as LogScreeningOptionKey
    if (!(key in DEFAULT_OPTION_VALUES)) continue
    ;(result as Record<LogScreeningOptionKey, unknown>)[key] = parseStoredValue(
      key,
      opt.value
    ) as never
  }
  return result
}

export function ScreeningSettingsTab() {
  const { t } = useTranslation()
  const optionsQuery = useSystemOptions()
  const updateOption = useUpdateOption()

  const initial = useMemo(
    () => deriveOptionValues(optionsQuery.data?.data),
    [optionsQuery.data?.data]
  )

  // Editable copy: keep everything as its display form. JSON keys stay as raw
  // strings so the textarea validates them; number keys are kept as strings
  // and coerced on save.
  const [values, setValues] = useState<LogScreeningOptionValues>(initial)

  useEffect(() => {
    setValues(initial)
  }, [initial])

  const [saving, setSaving] = useState(false)

  const dirtyKeys = useMemo(() => {
    const keys = Object.keys(DEFAULT_OPTION_VALUES) as LogScreeningOptionKey[]
    return keys.filter((key) => {
      const a = values[key]
      const b = initial[key]
      return String(a) !== String(b)
    })
  }, [values, initial])

  // JSON validation across all JSON keys.
  const jsonErrors = useMemo(() => {
    const errors: Partial<Record<LogScreeningOptionKey, string>> = {}
    for (const key of Object.keys(DEFAULT_OPTION_VALUES) as LogScreeningOptionKey[]) {
      if (!isJsonOptionKey(key)) continue
      const err = validateJson(String(values[key] ?? ''))
      if (err) errors[key] = err
    }
    return errors
  }, [values])

  const hasJsonError = Object.keys(jsonErrors).length > 0
  const canSave = dirtyKeys.length > 0 && !hasJsonError && !saving

  const setValue = (key: LogScreeningOptionKey, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }) as LogScreeningOptionValues)
  }

  const handleSave = async () => {
    if (!canSave) return
    setSaving(true)
    try {
      for (const key of dirtyKeys) {
        const raw = values[key]
        let payload: string | boolean | number
        if (isBooleanKey(key)) {
          payload = Boolean(raw)
        } else if (NUMBER_OPTION_KEYS.has(key)) {
          payload = Number(raw) || 0
        } else {
          payload = String(raw ?? '')
        }
        // The updateOption hook shows its own success/error toast and
        // invalidates the system-options query.
        await updateOption.mutateAsync({ key, value: payload })
      }
    } finally {
      setSaving(false)
    }
  }

  if (optionsQuery.isLoading) {
    return (
      <div className='text-muted-foreground flex items-center gap-2 text-sm'>
        <Loader2 className='size-4 animate-spin' />
        {t('Loading...')}
      </div>
    )
  }

  if (optionsQuery.isError || !optionsQuery.data) {
    return (
      <div className='text-muted-foreground text-sm'>
        {t('Failed to load settings')}
      </div>
    )
  }

  return (
    <div className='flex h-full min-h-0 flex-col gap-4'>
      <div className='flex items-center justify-end gap-2'>
        <Button onClick={handleSave} disabled={!canSave}>
          {saving ? (
            <Loader2 className='size-4 animate-spin' />
          ) : (
            <Save className='size-4' />
          )}
          {t('Save changes')}
        </Button>
      </div>

      <div className='min-h-0 flex-1 overflow-auto pr-1'>
        <div className='mx-auto flex max-w-3xl flex-col gap-6'>
          <SettingsGroup
            title={t('Sensitive Words')}
            description={t(
              'Core keyword filtering applied to prompts and completions.'
            )}
          >
            <SwitchRow
              label={t('Enable filtering')}
              description={t(
                'Blocks messages when sensitive keywords are detected.'
              )}
              checked={Boolean(values.CheckSensitiveEnabled)}
              onChange={(v) => setValue('CheckSensitiveEnabled', v)}
            />
            <SwitchRow
              label={t('Inspect user prompts')}
              description={t(
                'When enabled, prompts are scanned before reaching upstream models.'
              )}
              checked={Boolean(values.CheckSensitiveOnPromptEnabled)}
              onChange={(v) => setValue('CheckSensitiveOnPromptEnabled', v)}
            />
            <JsonTextarea
              label={t('Blocked keywords')}
              description={t(
                'Each line represents one keyword. Leave blank to disable the list but keep the switch states.'
              )}
              rows={8}
              value={String(values.SensitiveWords ?? '')}
              onChange={(v) => setValue('SensitiveWords', v)}
              mono
            />
          </SettingsGroup>

          <SettingsGroup
            title={t('Prompt Interception')}
            description={t(
              'Regex rules and response shaping for blocked prompts. Auto-ban here disables the user and their tokens locally — not an external joint ban.'
            )}
          >
            <JsonTextarea
              label={t('Prompt regex rules (JSON array)')}
              description={t(
                'Each rule: { pattern, rule_name, message, http_status_code, error_code, auto_ban }. auto_ban triggers local token-disable + user mark.'
              )}
              rows={8}
              value={String(values.SensitivePromptRegexRules ?? '[]')}
              onChange={(v) => setValue('SensitivePromptRegexRules', v)}
              error={jsonErrors.SensitivePromptRegexRules}
              mono
            />
            <InputRow
              label={t('Blocked message')}
              value={String(values.SensitivePromptBlockedMessage ?? '')}
              onChange={(v) => setValue('SensitivePromptBlockedMessage', v)}
            />
          </SettingsGroup>

          <SettingsGroup
            title={t('UA Interception')}
            description={t(
              'Block requests by User-Agent. Empty-UA blocking and local auto-ban are configured here.'
            )}
          >
            <SwitchRow
              label={t('Check User-Agent')}
              description={t(
                'When enabled, requests are inspected by User-Agent before relay.'
              )}
              checked={Boolean(values.CheckSensitiveOnUAEnabled)}
              onChange={(v) => setValue('CheckSensitiveOnUAEnabled', v)}
            />
            <JsonTextarea
              label={t('UA blocked regexes')}
              description={t('One regex per line. Case-insensitive when matched.')}
              rows={5}
              value={String(values.SensitiveUABlockedRegexes ?? '')}
              onChange={(v) => setValue('SensitiveUABlockedRegexes', v)}
              mono
            />
            <JsonTextarea
              label={t('UA regex rules (JSON array)')}
              description={t(
                'Each rule: { pattern, rule_name, message, http_status_code, error_code, auto_ban }. auto_ban triggers local token-disable + user mark.'
              )}
              rows={8}
              value={String(values.SensitiveUARegexRules ?? '[]')}
              onChange={(v) => setValue('SensitiveUARegexRules', v)}
              error={jsonErrors.SensitiveUARegexRules}
              mono
            />
            <JsonTextarea
              label={t('UA group regex rules (JSON object)')}
              description={t(
                'JSON object keyed by group name. Each value is a rule array, for example: { "vip": [{ "pattern": "curl", "message": "..." }] }. Token group is checked first, then user group.'
              )}
              rows={8}
              value={String(values.SensitiveUAGroupRegexRules ?? '{}')}
              onChange={(v) => setValue('SensitiveUAGroupRegexRules', v)}
              error={jsonErrors.SensitiveUAGroupRegexRules}
              mono
            />
            <InputRow
              label={t('Blocked message')}
              value={String(values.SensitiveUABlockedMessage ?? '')}
              onChange={(v) => setValue('SensitiveUABlockedMessage', v)}
            />
          </SettingsGroup>

          <SettingsGroup
            title={t('Empty UA Blocking')}
            description={t(
              'Block requests that send no User-Agent header. Auto-ban here is local only.'
            )}
          >
            <SwitchRow
              label={t('Block empty User-Agent')}
              description={t(
                'When enabled, requests with an empty User-Agent are rejected.'
              )}
              checked={Boolean(values.CheckSensitiveOnEmptyUAEnabled)}
              onChange={(v) => setValue('CheckSensitiveOnEmptyUAEnabled', v)}
            />
            <SwitchRow
              label={t('Local auto-ban on empty UA')}
              description={t(
                'Disables the user and their tokens locally when an empty-UA hit occurs. No external joint ban.'
              )}
              checked={Boolean(values.CheckSensitiveOnEmptyUAAutoBanEnabled)}
              onChange={(v) =>
                setValue('CheckSensitiveOnEmptyUAAutoBanEnabled', v)
              }
            />
            <InputRow
              label={t('Blocked message')}
              value={String(values.SensitiveEmptyUABlockedMessage ?? '')}
              onChange={(v) => setValue('SensitiveEmptyUABlockedMessage', v)}
            />
            <div className='grid grid-cols-2 gap-3'>
              <InputRow
                label={t('HTTP status code')}
                value={String(
                  values.SensitiveEmptyUABlockedHTTPStatusCode ?? ''
                )}
                onChange={(v) =>
                  setValue('SensitiveEmptyUABlockedHTTPStatusCode', v)
                }
                type='number'
              />
              <InputRow
                label={t('Error code')}
                value={String(values.SensitiveEmptyUABlockedErrorCode ?? '')}
                onChange={(v) =>
                  setValue('SensitiveEmptyUABlockedErrorCode', v)
                }
              />
            </div>
          </SettingsGroup>

          <SettingsGroup
            title={t('Log Screening Rules (JSON)')}
            description={t(
              'Aggregation rules for the screening run. Each rule has a window (1h / 24h), thresholds, param rules and UA blacklists.'
            )}
          >
            <JsonTextarea
              label={t('log_screening')}
              description={t(
                'JSON object: { enabled, rules[], expire_days }. Invalid JSON is rejected before saving.'
              )}
              rows={10}
              value={String(values.log_screening ?? '')}
              onChange={(v) => setValue('log_screening', v)}
              error={jsonErrors.log_screening}
              mono
            />
          </SettingsGroup>

          <SettingsGroup
            title={t('Request Param Recording (JSON)')}
            description={t(
              'Controls which request parameters are recorded for screening detail and block logs.'
            )}
          >
            <JsonTextarea
              label={t('relay_param_record')}
              description={t(
                'JSON object registered via GlobalConfig. Invalid JSON is rejected before saving.'
              )}
              rows={10}
              value={String(values.relay_param_record ?? '')}
              onChange={(v) => setValue('relay_param_record', v)}
              error={jsonErrors.relay_param_record}
              mono
            />
          </SettingsGroup>
        </div>
      </div>
    </div>
  )
}

function SettingsGroup(props: {
  title: string
  description?: string
  children: React.ReactNode
}) {
  return (
    <section className='bg-card flex flex-col gap-4 rounded-xl border p-4'>
      <div className='flex flex-col gap-1'>
        <h3 className='text-base font-semibold'>{props.title}</h3>
        {props.description && (
          <p className='text-muted-foreground text-xs'>{props.description}</p>
        )}
      </div>
      <div className='flex flex-col gap-4'>{props.children}</div>
    </section>
  )
}

function SwitchRow(props: {
  label: string
  description?: string
  checked: boolean
  onChange: (value: boolean) => void
}) {
  return (
    <div className='flex items-start justify-between gap-3'>
      <div className='flex flex-col gap-0.5'>
        <Label className='text-sm font-medium'>{props.label}</Label>
        {props.description && (
          <p className='text-muted-foreground text-xs'>{props.description}</p>
        )}
      </div>
      <Switch checked={props.checked} onCheckedChange={props.onChange} />
    </div>
  )
}

function InputRow(props: {
  label: string
  value: string
  onChange: (value: string) => void
  type?: 'text' | 'number'
}) {
  return (
    <div className='flex flex-col gap-1'>
      <Label className='text-sm font-medium'>{props.label}</Label>
      <Input
        type={props.type ?? 'text'}
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
      />
    </div>
  )
}

function JsonTextarea(props: {
  label: string
  description?: string
  value: string
  onChange: (value: string) => void
  rows?: number
  mono?: boolean
  error?: string
}) {
  const { t } = useTranslation()
  const textareaClass = cn(
    props.mono && 'font-mono text-xs',
    props.error && 'border-destructive ring-destructive/20'
  )
  return (
    <div className='flex flex-col gap-1'>
      <Label className='text-sm font-medium'>{props.label}</Label>
      {props.description && (
        <p className='text-muted-foreground text-xs'>{props.description}</p>
      )}
      <Textarea
        rows={props.rows ?? 6}
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
        spellCheck={false}
        className={textareaClass}
      />
      {props.error && (
        <p className='text-destructive text-xs'>
          {t('Invalid JSON')}: {props.error}
        </p>
      )}
    </div>
  )
}
