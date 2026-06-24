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
 * Type definitions for operations stats (Phase 4 admin-only read-only pages)
 */

// ============================================================================
// Common response wrapper
// ============================================================================

export interface OperationsStatsResponse<T> {
  success: boolean
  message?: string
  data?: T
}

// ============================================================================
// IP Stats
// ============================================================================

export interface IPStatsRankItem {
  ip: string
  count: number
}

export interface IPStatsRankData {
  kind: string
  request_path: string
  start_timestamp: number
  end_timestamp: number
  range_days: number
  limit: number
  total_ips: number
  items: IPStatsRankItem[]
}

export interface IPStatsUserItem {
  user_id: number
  username: string
  display_name: string
  remark: string
  count: number
  last_seen: number
}

export interface IPStatsUsersData {
  kind: string
  request_path: string
  ip: string
  start_timestamp: number
  end_timestamp: number
  range_days: number
  page: number
  page_size: number
  total: number
  items: IPStatsUserItem[]
}

// ============================================================================
// User Agent Stats
// ============================================================================

export interface UAStatsRankItem {
  user_agent: string
  count: number
}

export interface UAStatsRankData {
  start_timestamp: number
  end_timestamp: number
  range_days: number
  keyword: string
  limit: number
  total_uas: number
  items: UAStatsRankItem[]
}

export type UAMatchMode = 'exact' | 'contains'

export interface UAStatsUserItem {
  user_id: number
  username: string
  display_name: string
  remark: string
  count: number
  last_seen: number
}

export interface UAStatsUsersData {
  ua: string
  match: UAMatchMode
  start_timestamp: number
  end_timestamp: number
  range_days: number
  page: number
  page_size: number
  total: number
  items: UAStatsUserItem[]
}

// ============================================================================
// User Leaderboard
// ============================================================================

export type LeaderboardMetric = 'calls' | 'quota' | 'rph'

export interface LeaderboardRankItem {
  user_id: number
  username: string
  display_name: string
  remark: string
  call_count: number
  quota_sum: number
  rph: number
  first_call: number
  last_call: number
}

export interface LeaderboardRankData {
  metric: LeaderboardMetric
  start_timestamp: number
  end_timestamp: number
  range_days: number
  limit: number
  items: LeaderboardRankItem[]
}

export interface LeaderboardCoverageItem {
  user_id: number
  username: string
  display_name: string
  remark: string
  active_slots: number
  total_slots: number
  coverage_pct: number
}

export interface LeaderboardCoverageData {
  slot_minutes: number
  start_timestamp: number
  end_timestamp: number
  range_days: number
  limit: number
  items: LeaderboardCoverageItem[]
}

// ============================================================================
// Key Lookup
// ============================================================================

export interface KeyLookupUser {
  id: number
  username: string
  display_name?: string
  role?: number
  group?: string
  quota?: number
  used_quota?: number
}

export interface KeyLookupToken {
  id: number
  user_id: number
  key: string
  name?: string
  status: number
  created_time: number
  accessed_time?: number
  expired_time?: number
  remain_quota: number
  used_quota: number
  unlimited_quota: boolean
  group?: string
}

export interface KeyLookupData {
  user: KeyLookupUser
  token: KeyLookupToken
  key_masked: string
}
