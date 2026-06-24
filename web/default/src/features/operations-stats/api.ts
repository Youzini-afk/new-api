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
 * API functions for operations stats (Phase 4 admin-only read-only pages)
 */
import { api } from '@/lib/api'
import type {
  IPStatsRankData,
  IPStatsUsersData,
  KeyLookupData,
  LeaderboardCoverageData,
  LeaderboardRankData,
  OperationsStatsResponse,
  UAStatsRankData,
  UAStatsUsersData,
  UAMatchMode,
} from './types'

// ============================================================================
// IP Stats
// ============================================================================

interface GetIPStatsRankParams {
  kind?: string
  range_days?: number
  start_timestamp?: number
  end_timestamp?: number
  limit?: number
}

export function getIPStatsRank(
  params: GetIPStatsRankParams
): Promise<OperationsStatsResponse<IPStatsRankData>> {
  return api
    .get('/api/ip_stats/conversation/rank', { params })
    .then((res) => res.data)
}

interface GetIPStatsUsersParams {
  ip: string
  kind?: string
  range_days?: number
  start_timestamp?: number
  end_timestamp?: number
  p?: number
  page_size?: number
}

export function getIPStatsUsers(
  params: GetIPStatsUsersParams
): Promise<OperationsStatsResponse<IPStatsUsersData>> {
  return api
    .get('/api/ip_stats/conversation/users', { params })
    .then((res) => res.data)
}

// ============================================================================
// User Agent Stats
// ============================================================================

interface GetUAStatsRankParams {
  range_days?: number
  start_timestamp?: number
  end_timestamp?: number
  keyword?: string
  limit?: number
}

export function getUAStatsRank(
  params: GetUAStatsRankParams
): Promise<OperationsStatsResponse<UAStatsRankData>> {
  return api.get('/api/ua_stats/rank', { params }).then((res) => res.data)
}

interface GetUAStatsUsersParams {
  ua: string
  match?: UAMatchMode
  range_days?: number
  start_timestamp?: number
  end_timestamp?: number
  p?: number
  page_size?: number
}

export function getUAStatsUsers(
  params: GetUAStatsUsersParams
): Promise<OperationsStatsResponse<UAStatsUsersData>> {
  return api.get('/api/ua_stats/users', { params }).then((res) => res.data)
}

// ============================================================================
// User Leaderboard
// ============================================================================

interface GetLeaderboardRankParams {
  metric?: string
  range_days?: number
  start_timestamp?: number
  end_timestamp?: number
  limit?: number
}

export function getUserLeaderboardRank(
  params: GetLeaderboardRankParams
): Promise<OperationsStatsResponse<LeaderboardRankData>> {
  return api
    .get('/api/user_leaderboard/rank', { params })
    .then((res) => res.data)
}

interface GetLeaderboardCoverageParams {
  range_days?: number
  start_timestamp?: number
  end_timestamp?: number
  slot_minutes?: number
  limit?: number
}

export function getUserLeaderboardCoverage(
  params: GetLeaderboardCoverageParams
): Promise<OperationsStatsResponse<LeaderboardCoverageData>> {
  return api
    .get('/api/user_leaderboard/coverage', { params })
    .then((res) => res.data)
}

// ============================================================================
// Key Lookup
// ============================================================================

export function lookupKey(
  key: string
): Promise<OperationsStatsResponse<KeyLookupData>> {
  return api.get('/api/key_lookup', { params: { key } }).then((res) => res.data)
}
