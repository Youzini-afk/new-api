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
 * Shared constants for the log screening feature.
 */

// ============================================================================
// Tabs
// ============================================================================

export type LogScreeningTabId =
  | 'cases'
  | 'records'
  | 'prompt-blocks'
  | 'ua-blocks'
  | 'risk-settings'
  | 'settings'

export const LOG_SCREENING_TABS: {
  id: LogScreeningTabId
  titleKey: string
}[] = [
  { id: 'cases', titleKey: 'Risk Cases' },
  { id: 'records', titleKey: 'Screening Records' },
  { id: 'prompt-blocks', titleKey: 'Prompt Blocks' },
  { id: 'ua-blocks', titleKey: 'UA Blocks' },
  { id: 'risk-settings', titleKey: 'Risk Settings' },
  { id: 'settings', titleKey: 'Interception Settings' },
]

export const LOG_SCREENING_DEFAULT_TAB: LogScreeningTabId = 'cases'

// ============================================================================
// Screening run kinds
// ============================================================================

export type ScreeningKind = 'chat_completions'

export const SCREENING_KINDS: { value: ScreeningKind; labelKey: string }[] = [
  { value: 'chat_completions', labelKey: 'Chat Completions' },
]

export const DEFAULT_SCREENING_KIND: ScreeningKind = 'chat_completions'

// ============================================================================
// Tristate filter ("any" / "yes" / "no")
// ============================================================================

export type TristateFilter = 'any' | 'yes' | 'no'

export const TRISTATE_OPTIONS: {
  value: TristateFilter
  labelKey: string
}[] = [
  { value: 'any', labelKey: 'Any' },
  { value: 'yes', labelKey: 'Yes' },
  { value: 'no', labelKey: 'No' },
]

/** Convert a tristate filter into the backend query value ("" / "1" / "0"). */
export function tristateToQuery(value: TristateFilter): string | undefined {
  if (value === 'any') return undefined
  return value === 'yes' ? '1' : '0'
}

// ============================================================================
// Risk level styling
// ============================================================================

export type RiskLevel = 'critical' | 'high' | 'medium' | 'low' | 'info'

export interface RiskLevelConfig {
  labelKey: string
  badgeClass: string
}

const RISK_LEVEL_MAP: Record<string, RiskLevelConfig> = {
  critical: {
    labelKey: 'Critical',
    badgeClass:
      'bg-destructive/15 text-destructive border-destructive/30 dark:bg-destructive/20',
  },
  high: {
    labelKey: 'High',
    badgeClass:
      'bg-orange-500/15 text-orange-600 border-orange-500/30 dark:text-orange-400 dark:bg-orange-500/20',
  },
  medium: {
    labelKey: 'Medium',
    badgeClass:
      'bg-amber-500/15 text-amber-600 border-amber-500/30 dark:text-amber-400 dark:bg-amber-500/20',
  },
  low: {
    labelKey: 'Low',
    badgeClass:
      'bg-sky-500/15 text-sky-600 border-sky-500/30 dark:text-sky-400 dark:bg-sky-500/20',
  },
  info: {
    labelKey: 'Info',
    badgeClass: 'bg-muted text-muted-foreground border-border',
  },
}

const DEFAULT_RISK_LEVEL: RiskLevelConfig = RISK_LEVEL_MAP.info

/** Normalize a backend risk_level string into a stable badge config. */
export function getRiskLevelConfig(raw: string): RiskLevelConfig {
  const key = (raw || '').trim().toLowerCase()
  return RISK_LEVEL_MAP[key] ?? DEFAULT_RISK_LEVEL
}

// ============================================================================
// Pagination
// ============================================================================

export const DEFAULT_PAGE_SIZE = 20

export const PAGE_SIZE_OPTIONS = [10, 20, 50, 100] as const

// ============================================================================
// Option keys edited by the Settings tab.
// Boolean option keys are stored as "true"/"false" strings.
// ============================================================================

export const BOOLEAN_OPTION_KEYS: readonly (keyof import('./types').LogScreeningOptionValues)[] =
  [
    'CheckSensitiveEnabled',
    'CheckSensitiveOnPromptEnabled',
    'CheckSensitiveOnUAEnabled',
    'CheckSensitiveOnEmptyUAEnabled',
    'CheckSensitiveOnEmptyUAAutoBanEnabled',
  ] as const

/** Option keys whose value is a JSON document edited through a validating textarea. */
export const JSON_OPTION_KEYS: readonly (keyof import('./types').LogScreeningOptionValues)[] =
  [
    'SensitivePromptRegexRules',
    'SensitiveUARegexRules',
    'SensitiveUAGroupRegexRules',
    'log_screening',
    'relay_param_record',
  ] as const

export function isJsonOptionKey(
  key: keyof import('./types').LogScreeningOptionValues
): boolean {
  return JSON_OPTION_KEYS.includes(key)
}
