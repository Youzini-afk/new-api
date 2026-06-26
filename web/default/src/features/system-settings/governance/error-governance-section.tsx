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

import { SettingsSwitchField } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  RELAY_ERROR_DEFAULT_MESSAGES,
  RELAY_ERROR_GOVERNANCE_OPTION_KEY,
  RELAY_ERROR_RULE_HINTS,
  RELAY_ERROR_RULE_LABELS,
  buildGovernanceRows,
  serializeGovernanceConfig,
} from './constants'
import type { GovernanceRuleRow, RelayErrorGovernanceConfig } from './types'

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
    }
  } catch {
    return null
  }
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
  const parsedRows = useMemo(() => buildGovernanceRows(parsed), [parsed])

  // Draft state — null until the admin edits something, so server values stay
  // authoritative until a change is made (matches the api-info section pattern).
  const [draftEnabled, setDraftEnabled] = useState<boolean | null>(null)
  const [draftRows, setDraftRows] = useState<GovernanceRuleRow[] | null>(null)

  // Reset drafts whenever the underlying option is refreshed after a save.
  useEffect(() => {
    setDraftEnabled(null)
    setDraftRows(null)
  }, [defaultValue])

  const effectiveEnabled = draftEnabled ?? parsed?.enabled ?? false
  const effectiveRows = draftRows ?? parsedRows
  const hasChanges = draftEnabled !== null || draftRows !== null

  const updateRow = (code: string, patch: Partial<GovernanceRuleRow>) => {
    setDraftRows((draft) =>
      (draft ?? effectiveRows.map((row) => ({ ...row }))).map((row) =>
        row.code === code ? { ...row, ...patch } : row
      )
    )
  }

  const handleReset = () => {
    setDraftEnabled(null)
    setDraftRows(null)
  }

  const handleSave = async () => {
    const config = serializeGovernanceConfig(effectiveEnabled, effectiveRows)
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
          'Rule codes are fixed by the backend and cannot be edited here. Only the enabled flag and custom message can be overridden; leave the message blank to use the default.'
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
              const label = RELAY_ERROR_RULE_LABELS[row.code] ?? row.code
              const hint = RELAY_ERROR_RULE_HINTS[row.code] ?? ''
              return (
                <TableRow key={row.code}>
                  <TableCell>
                    <div className='flex flex-col gap-1'>
                      <Badge variant='outline' className='w-fit font-mono'>
                        {row.code}
                      </Badge>
                      <span className='text-sm font-medium'>{label}</span>
                      {hint && (
                        <span className='text-muted-foreground text-xs'>
                          {hint}
                        </span>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className='text-muted-foreground text-sm'>
                    <span
                      className='line-clamp-2 max-w-[280px]'
                      title={defaultMessage}
                    >
                      {defaultMessage}
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
                    <Input
                      value={row.message}
                      onChange={(e) =>
                        updateRow(row.code, { message: e.target.value })
                      }
                      placeholder={defaultMessage}
                      aria-label={t('Custom message for {{code}}', {
                        code: row.code,
                      })}
                    />
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
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
