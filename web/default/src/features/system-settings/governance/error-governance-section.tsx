import { ArrowDown, ArrowUp, RefreshCw, Trash2 } from 'lucide-react'
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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
  GovernanceRuleRow,
  RelayErrorCustomRule,
  RelayErrorGovernanceConfig,
} from './types'

type ErrorGovernanceSectionProps = {
  /** Raw JSON string of the `relay_error_governance` system option. */
  defaultValue: string
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
          <Badge variant='secondary'>{customRules.length}</Badge>
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
    </SettingsSection>
  )
}
