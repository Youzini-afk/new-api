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
 * Shared constants for the error insight feature.
 */

// ============================================================================
// Tabs
// ============================================================================

export type ErrorInsightTabId = 'signatures' | 'logs'

export const ERROR_INSIGHT_TABS: {
  id: ErrorInsightTabId
  titleKey: string
}[] = [
  { id: 'signatures', titleKey: 'Signatures' },
  { id: 'logs', titleKey: 'Logs' },
]

export const ERROR_INSIGHT_DEFAULT_TAB: ErrorInsightTabId = 'signatures'

// ============================================================================
// Time range presets (in seconds relative to now; value 0 = all time)
// ============================================================================

export const TIME_RANGE_PRESETS = [
  { value: 3600, labelKey: '1 Hour' },
  { value: 86400, labelKey: '24 Hours' },
  { value: 604800, labelKey: '7 Days' },
  { value: 2592000, labelKey: '30 Days' },
  { value: 0, labelKey: 'All Time' },
] as const

export type TimeRangePreset = (typeof TIME_RANGE_PRESETS)[number]['value']

export const DEFAULT_TIME_RANGE_PRESET: TimeRangePreset = 86400

// ============================================================================
// Rule matched filter
// ============================================================================

export type RuleMatchedFilter = 'all' | 'matched' | 'unmatched'

export const RULE_MATCHED_OPTIONS: {
  value: RuleMatchedFilter
  labelKey: string
}[] = [
  { value: 'all', labelKey: 'All' },
  { value: 'matched', labelKey: 'Matched' },
  { value: 'unmatched', labelKey: 'Unmatched' },
]

// ============================================================================
// Pagination
// ============================================================================

export const DEFAULT_PAGE_SIZE = 20

export const PAGE_SIZE_OPTIONS = [20, 50, 100] as const
