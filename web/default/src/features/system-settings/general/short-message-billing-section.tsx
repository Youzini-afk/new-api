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
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import * as z from 'zod'
import {
  ChevronDown,
  Plus,
  ShieldAlert,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const OPTION_KEY = 'quota_setting.short_msg_extra_billing'

type ShortMsgMode = 'off' | 'shadow' | 'enforce'

type ShortMsgRule = {
  clientKey?: string
  id: string
  group: string
  trigger: 'input_tokens_below'
  threshold: number
  fee_quota: number
  waive_when_completion_tokens_zero: boolean
}

type ShortMsgConfig = {
  mode: ShortMsgMode
  rules: ShortMsgRule[]
}

const DEFAULT_CONFIG: ShortMsgConfig = { mode: 'off', rules: [] }

const RULE_FORM_ID = 'short-msg-extra-billing-form'

let nextRuleClientKey = 0

function createRuleClientKey() {
  nextRuleClientKey += 1
  return `short-msg-rule-${nextRuleClientKey}`
}

const ruleSchema = z.object({
  id: z.string().trim().min(1),
  group: z.string().trim().min(1),
  trigger: z.literal('input_tokens_below'),
  threshold: z.number().int().positive(),
  fee_quota: z.number().int().positive(),
  waive_when_completion_tokens_zero: z.boolean(),
})

const configSchema = z
  .object({
    mode: z.enum(['off', 'shadow', 'enforce']),
    rules: z.array(ruleSchema),
  })
  .superRefine((value, ctx) => {
    const seen = new Set<string>()
    value.rules.forEach((rule, index) => {
      if (rule.id && seen.has(rule.id)) {
        ctx.addIssue({
          code: 'custom',
          message: `Duplicate rule id: ${rule.id}`,
          path: ['rules', index, 'id'],
        })
      }
      if (rule.id) seen.add(rule.id)
    })
  })

function normalizeRule(raw: unknown): ShortMsgRule | null {
  if (!raw || typeof raw !== 'object') return null
  const src = raw as Record<string, unknown>
  const id = typeof src.id === 'string' ? src.id : ''
  const group =
    typeof src.group === 'string'
      ? src.group
      : typeof src.model === 'string'
        ? src.model
        : ''
  const threshold =
    typeof src.threshold === 'number' && Number.isFinite(src.threshold)
      ? src.threshold
      : 0
  const feeQuota =
    typeof src.fee_quota === 'number' && Number.isFinite(src.fee_quota)
      ? src.fee_quota
      : 0
  const waive =
    typeof src.waive_when_completion_tokens_zero === 'boolean'
      ? src.waive_when_completion_tokens_zero
      : true
  return {
    clientKey: createRuleClientKey(),
    id,
    group,
    trigger: 'input_tokens_below',
    threshold,
    fee_quota: feeQuota,
    waive_when_completion_tokens_zero: waive,
  }
}

function parseConfig(raw: string | undefined): ShortMsgConfig {
  if (!raw) return { ...DEFAULT_CONFIG }
  try {
    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return { ...DEFAULT_CONFIG }
    }
    const obj = parsed as Record<string, unknown>
    const mode = obj.mode
    const safeMode: ShortMsgMode =
      mode === 'shadow' || mode === 'enforce' ? mode : 'off'
    const rawRules = Array.isArray(obj.rules) ? obj.rules : []
    const rules = rawRules
      .map(normalizeRule)
      .filter((r): r is ShortMsgRule => r !== null)
    return { mode: safeMode, rules }
  } catch {
    return { ...DEFAULT_CONFIG }
  }
}

function serializeConfig(config: ShortMsgConfig): string {
  const normalized: ShortMsgConfig = {
    mode: config.mode,
    rules: config.rules.map((rule) => ({
      id: rule.id.trim(),
      group: rule.group.trim(),
      trigger: 'input_tokens_below',
      threshold: rule.threshold,
      fee_quota: rule.fee_quota,
      waive_when_completion_tokens_zero: rule.waive_when_completion_tokens_zero,
    })),
  }
  return JSON.stringify(normalized)
}

type ShortMessageBillingSectionProps = {
  defaultValue: string
}

export function ShortMessageBillingSection({
  defaultValue,
}: ShortMessageBillingSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const initialConfig = useMemo(
    () => parseConfig(defaultValue),
    [defaultValue]
  )
  const initialSerializedRef = useRef<string>(serializeConfig(initialConfig))

  const [config, setConfig] = useState<ShortMsgConfig>(initialConfig)
  const [validationError, setValidationError] = useState<string>('')

  useEffect(() => {
    const next = parseConfig(defaultValue)
    initialSerializedRef.current = serializeConfig(next)
    setConfig(next)
    setValidationError('')
  }, [defaultValue])

  const serialized = useMemo(() => serializeConfig(config), [config])
  const isDirty = serialized !== initialSerializedRef.current

  const updateMode = useCallback((mode: ShortMsgMode) => {
    setConfig((prev) => ({ ...prev, mode }))
    setValidationError('')
  }, [])

  const addRule = useCallback(() => {
    setConfig((prev) => {
      const baseId = 'rule'
      const existing = new Set(prev.rules.map((r) => r.id))
      let index = prev.rules.length + 1
      let id = `${baseId}-${index}`
      while (existing.has(id)) {
        index += 1
        id = `${baseId}-${index}`
      }
      const newRule: ShortMsgRule = {
        clientKey: createRuleClientKey(),
        id,
        group: 'default',
        trigger: 'input_tokens_below',
        threshold: 8,
        fee_quota: 500,
        waive_when_completion_tokens_zero: true,
      }
      return { ...prev, rules: [...prev.rules, newRule] }
    })
    setValidationError('')
  }, [])

  const removeRule = useCallback((clientKey: string) => {
    setConfig((prev) => ({
      ...prev,
      rules: prev.rules.filter((r) => r.clientKey !== clientKey),
    }))
    setValidationError('')
  }, [])

  const updateRule = useCallback(
    (clientKey: string, patch: Partial<ShortMsgRule>) => {
      setConfig((prev) => ({
        ...prev,
        rules: prev.rules.map((r) =>
          r.clientKey === clientKey ? { ...r, ...patch } : r
        ),
      }))
      setValidationError('')
    },
    []
  )

  const handleSave = useCallback(async () => {
    const result = configSchema.safeParse(config)
    if (!result.success) {
      const firstIssue = result.error.issues[0]
      const message = firstIssue
        ? `${firstIssue.path.join('.') || 'config'}: ${firstIssue.message}`
        : t('Invalid configuration')
      setValidationError(message)
      toast.error(t('Please fix the errors before saving'))
      return
    }

    const nextSerialized = serializeConfig(result.data)
    if (nextSerialized === initialSerializedRef.current) {
      toast.info(t('No changes to save'))
      return
    }

    await updateOption.mutateAsync({
      key: OPTION_KEY,
      value: nextSerialized,
    })
    initialSerializedRef.current = nextSerialized
    setValidationError('')
  }, [config, t, updateOption])

  const modeOptions: Array<{
    value: ShortMsgMode
    titleKey: string
    descriptionKey: string
    destructive?: boolean
  }> = [
    {
      value: 'off',
      titleKey: 'Disabled',
      descriptionKey:
        'No extra billing. Rules are preserved but have no effect.',
    },
    {
      value: 'shadow',
      titleKey: 'Shadow (Audit Only)',
      descriptionKey:
        'Records potential extra charges in consume logs without charging users.',
    },
    {
      value: 'enforce',
      titleKey: 'Enforce (Charge)',
      descriptionKey:
        'Reserves potential extra quota before the upstream call and charges it only if usage still matches.',
      destructive: true,
    },
  ]

  const rulesDisabled = config.mode === 'off'

  return (
    <SettingsSection title={t('Short Message Billing')}>
      <FormNavigationGuard when={isDirty} />
      <FormDirtyIndicator isDirty={isDirty} />

      <Alert>
        <AlertDescription className='space-y-1 text-sm'>
          <div>
            {t(
              'Apply an extra quota charge to short prompts that fall below a token threshold, regardless of completion length.'
            )}
          </div>
          <div className='text-muted-foreground'>
            {t(
              'Rule fires only when input/prompt tokens are strictly below the threshold. Fee quota is raw quota units, not currency.'
            )}
          </div>
        </AlertDescription>
      </Alert>

      <form
        id={RULE_FORM_ID}
        onSubmit={(e) => {
          e.preventDefault()
          void handleSave()
        }}
        className='grid min-w-0 gap-y-6 lg:grid-cols-2 lg:[&>*]:col-span-2'
      >
        <SettingsPageFormActions
          onSave={handleSave}
          isSaving={updateOption.isPending}
          isSaveDisabled={!isDirty && !validationError}
        />

        <div className='grid gap-2'>
          <Label>{t('Billing Mode')}</Label>
          <RadioGroup
            value={config.mode}
            onValueChange={(value) => updateMode(value as ShortMsgMode)}
            className='grid gap-3 sm:grid-cols-3'
          >
            {modeOptions.map((option) => {
              const selected = config.mode === option.value
              return (
                <Label
                  key={option.value}
                  htmlFor={`short-msg-mode-${option.value}`}
                  className={cn(
                    'group bg-card border-muted flex cursor-pointer flex-col gap-2 rounded-xl border p-4 font-normal transition-all',
                    'hover:border-primary/40 focus-within:border-primary/50',
                    'has-data-[checked]:ring-2',
                    option.destructive && selected
                      ? 'border-destructive has-data-[checked]:border-destructive has-data-[checked]:ring-destructive/20'
                      : 'has-data-[checked]:border-primary has-data-[checked]:ring-primary/20'
                  )}
                >
                  <div className='flex items-center gap-3'>
                    <RadioGroupItem
                      id={`short-msg-mode-${option.value}`}
                      value={option.value}
                      className='mt-1'
                    />
                    <div>
                      <Label
                        htmlFor={`short-msg-mode-${option.value}`}
                        className={cn(
                          'text-base leading-none font-semibold',
                          option.destructive && selected && 'text-destructive'
                        )}
                      >
                        {t(option.titleKey)}
                      </Label>
                      <p className='text-muted-foreground mt-2 text-sm'>
                        {t(option.descriptionKey)}
                      </p>
                    </div>
                    {option.destructive ? (
                      <ShieldAlert
                        className={cn(
                          'ml-auto size-5 shrink-0 transition',
                          selected
                            ? 'text-destructive'
                            : 'text-muted-foreground/70 group-hover:text-destructive'
                        )}
                      />
                    ) : null}
                  </div>
                </Label>
              )
            })}
          </RadioGroup>
        </div>

        {config.mode === 'enforce' ? (
          <Alert variant='destructive'>
            <ShieldAlert />
            <AlertTitle>{t('Enforce mode is high-risk')}</AlertTitle>
            <AlertDescription className='space-y-1'>
              <div>
                {t(
                  'Enforce reserves the potential extra quota before the upstream call using a frozen rule, then charges the extra fee only if the actual usage still matches the trigger.'
                )}
              </div>
              <div>
                {t(
                  'Misconfigured thresholds or volatile tokenizers can over-charge users. Enable only after validating with shadow mode.'
                )}
              </div>
            </AlertDescription>
          </Alert>
        ) : null}

        {rulesDisabled ? (
          <Alert>
            <AlertDescription>
              {t(
                'Rules are preserved while billing is disabled. Switch to Shadow or Enforce to activate them.'
              )}
            </AlertDescription>
          </Alert>
        ) : null}

        <div
          className={cn(
            'flex min-w-0 flex-col gap-3 rounded-xl border px-3 py-3 transition',
            rulesDisabled ? 'border-dashed opacity-60' : 'border-border/70'
          )}
        >
          <div className='flex items-center justify-between gap-2'>
            <div className='flex flex-col gap-0.5'>
              <Label className='text-sm font-semibold'>
                {t('Billing Rules')}
              </Label>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Each rule targets one token/user group. Multiple rules cannot share the same id.'
                )}
              </p>
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={addRule}
              disabled={rulesDisabled}
            >
              <Plus className='mr-1 h-4 w-4' />
              {t('Add Rule')}
            </Button>
          </div>

          {config.rules.length === 0 ? (
            <p className='text-muted-foreground py-6 text-center text-sm'>
              {t('No rules configured. Add a rule to define short-message fees.')}
            </p>
          ) : (
            <div className='flex min-w-0 flex-col gap-3'>
              {config.rules.map((rule) => (
                <ShortMsgRuleRow
                  key={rule.clientKey}
                  rule={rule}
                  disabled={rulesDisabled}
                  onChange={(patch) => updateRule(rule.clientKey ?? '', patch)}
                  onRemove={() => removeRule(rule.clientKey ?? '')}
                />
              ))}
            </div>
          )}
        </div>

        {validationError ? (
          <Alert variant='destructive'>
            <AlertDescription>{validationError}</AlertDescription>
          </Alert>
        ) : null}

        <Collapsible>
          <CollapsibleTrigger
            render={
              <Button
                type='button'
                variant='ghost'
                size='sm'
                className='w-full justify-start'
              />
            }
          >
            <ChevronDown className='mr-2 h-4 w-4' />
            {t('JSON Preview (read-only)')}
          </CollapsibleTrigger>
          <CollapsibleContent className='pt-2'>
            <Textarea
              readOnly
              value={JSON.stringify(config, null, 2)}
              rows={10}
              spellCheck={false}
              className='font-mono text-xs'
              aria-label={t('Serialized configuration preview')}
            />
          </CollapsibleContent>
        </Collapsible>
      </form>
    </SettingsSection>
  )
}

type ShortMsgRuleRowProps = {
  rule: ShortMsgRule
  disabled: boolean
  onChange: (patch: Partial<ShortMsgRule>) => void
  onRemove: () => void
}

function ShortMsgRuleRow(props: ShortMsgRuleRowProps) {
  const { t } = useTranslation()
  const { rule, disabled } = props
  const rowKey = rule.clientKey ?? rule.id

  const handleThresholdChange = (raw: string) => {
    const parsed = raw === '' ? 0 : Number(raw)
    props.onChange({ threshold: Number.isFinite(parsed) ? Math.trunc(parsed) : 0 })
  }

  const handleFeeQuotaChange = (raw: string) => {
    const parsed = raw === '' ? 0 : Number(raw)
    props.onChange({ fee_quota: Number.isFinite(parsed) ? Math.trunc(parsed) : 0 })
  }

  return (
    <div className='bg-muted/20 flex min-w-0 flex-col gap-3 rounded-lg border px-3 py-3'>
      <div className='grid gap-3 sm:grid-cols-2'>
        <div className='grid gap-1.5'>
          <Label htmlFor={`rule-${rowKey}-id`}>{t('Rule ID')} *</Label>
          <Input
            id={`rule-${rowKey}-id`}
            value={rule.id}
            placeholder='cheap-short-prompt'
            disabled={disabled}
            onChange={(e) => props.onChange({ id: e.target.value })}
          />
        </div>
        <div className='grid gap-1.5'>
          <Label htmlFor={`rule-${rowKey}-group`}>{t('Group')} *</Label>
          <Input
            id={`rule-${rowKey}-group`}
            value={rule.group}
            placeholder='default'
            disabled={disabled}
            onChange={(e) => props.onChange({ group: e.target.value })}
          />
        </div>
      </div>

      <div className='grid gap-3 sm:grid-cols-3'>
        <div className='grid gap-1.5'>
          <Label>{t('Trigger')}</Label>
          <Input value='input_tokens_below' readOnly disabled={disabled} />
        </div>
        <div className='grid gap-1.5'>
          <Label htmlFor={`rule-${rowKey}-threshold`}>
            {t('Threshold (tokens)')} *
          </Label>
          <Input
            id={`rule-${rowKey}-threshold`}
            type='number'
            min={1}
            step={1}
            value={rule.threshold === 0 ? '' : rule.threshold}
            placeholder='8'
            disabled={disabled}
            onChange={(e) => handleThresholdChange(e.target.value)}
          />
        </div>
        <div className='grid gap-1.5'>
          <Label htmlFor={`rule-${rowKey}-fee`}>
            {t('Extra Fee (quota)')} *
          </Label>
          <Input
            id={`rule-${rowKey}-fee`}
            type='number'
            min={1}
            step={1}
            value={rule.fee_quota === 0 ? '' : rule.fee_quota}
            placeholder='500'
            disabled={disabled}
            onChange={(e) => handleFeeQuotaChange(e.target.value)}
          />
        </div>
      </div>

      <div className='flex items-center justify-between gap-2 pt-1'>
        <Label
          htmlFor={`rule-${rowKey}-waive`}
          className='text-muted-foreground cursor-pointer font-normal'
        >
          <Checkbox
            id={`rule-${rowKey}-waive`}
            checked={rule.waive_when_completion_tokens_zero}
            disabled={disabled}
            onCheckedChange={(value) =>
              props.onChange({ waive_when_completion_tokens_zero: value === true })
            }
          />
          <span>
            {t(
              'Waive extra fee when completion tokens are zero (base quota still applies).'
            )}
          </span>
        </Label>
        <Button
          type='button'
          variant='ghost'
          size='icon'
          onClick={props.onRemove}
          disabled={disabled}
          aria-label={t('Delete rule')}
        >
          <Trash2 className='text-destructive h-4 w-4' />
        </Button>
      </div>
    </div>
  )
}
