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
 * Shared constants for operations stats feature
 */
import type { LeaderboardMetric, UAMatchMode } from './types'

// ============================================================================
// Tabs
// ============================================================================

export type OperationsStatsTabId = 'ip' | 'ua' | 'leaderboard' | 'key-lookup'

export const OPERATIONS_STATS_TABS: {
  id: OperationsStatsTabId
  titleKey: string
}[] = [
  { id: 'ip', titleKey: 'IP Stats' },
  { id: 'ua', titleKey: 'User Agents' },
  { id: 'leaderboard', titleKey: 'User Leaderboard' },
  { id: 'key-lookup', titleKey: 'Key Lookup' },
]

export const OPERATIONS_STATS_DEFAULT_TAB: OperationsStatsTabId = 'ip'

// ============================================================================
// Conversation kinds for IP stats
// ============================================================================

export type ConversationKind = 'chat_completions' | 'responses' | 'messages'

export const CONVERSATION_KINDS: {
  value: ConversationKind
  labelKey: string
}[] = [
  { value: 'chat_completions', labelKey: 'Chat Completions' },
  { value: 'responses', labelKey: 'Responses' },
  { value: 'messages', labelKey: 'Messages' },
]

export const DEFAULT_CONVERSATION_KIND: ConversationKind = 'chat_completions'

// ============================================================================
// Leaderboard metrics
// ============================================================================

export const LEADERBOARD_METRICS: {
  value: LeaderboardMetric | 'coverage'
  labelKey: string
}[] = [
  { value: 'calls', labelKey: 'Calls' },
  { value: 'quota', labelKey: 'Quota' },
  { value: 'rph', labelKey: 'RPH' },
  { value: 'coverage', labelKey: 'Coverage' },
]

export const DEFAULT_LEADERBOARD_METRIC: LeaderboardMetric | 'coverage' = 'calls'

// ============================================================================
// UA match modes
// ============================================================================

export const UA_MATCH_MODES: { value: UAMatchMode; labelKey: string }[] = [
  { value: 'contains', labelKey: 'Contains' },
  { value: 'exact', labelKey: 'Exact' },
]

export const DEFAULT_UA_MATCH_MODE: UAMatchMode = 'contains'

// ============================================================================
// Time range presets (in days)
// ============================================================================

export const TIME_RANGE_PRESETS = [
  { value: 1, labelKey: '24 Hours' },
  { value: 7, labelKey: '7 Days' },
  { value: 14, labelKey: '14 Days' },
  { value: 30, labelKey: '30 Days' },
] as const

export type TimeRangePreset = (typeof TIME_RANGE_PRESETS)[number]['value']

// ============================================================================
// Rank limit options
// ============================================================================

export const RANK_LIMIT_OPTIONS = [100, 250, 500, 1000] as const

export const DEFAULT_RANK_LIMIT = 100

// ============================================================================
// Coverage slot minutes
// ============================================================================

export const COVERAGE_SLOT_OPTIONS = [5, 10, 15, 30, 60] as const

export const DEFAULT_COVERAGE_SLOT_MINUTES = 5

// ============================================================================
// Pagination
// ============================================================================

export const DEFAULT_PAGE_SIZE = 20

// ============================================================================
// Key lookup
// ============================================================================

export const KEY_LOOKUP_SK_PREFIX = 'sk-'
