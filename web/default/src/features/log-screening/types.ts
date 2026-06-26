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
 * Type definitions for log screening (Phase 5 admin-only pages).
 *
 * These mirror the backend DTOs exposed by the `/api/log_screening/*`
 * admin-only endpoints. `ban_sync` is intentionally not modeled: it is
 * deprecated for this branch, so no ban_sync surfaces appear here.
 */

// ============================================================================
// Common response wrapper
// ============================================================================

export interface LogScreeningResponse<T> {
  success: boolean
  message?: string
  data?: T
}

/** Paginated list payload shared by all list endpoints. */
export interface LogScreeningPage<T> {
  page: number
  page_size: number
  total: number
  items: T[]
}

// ============================================================================
// Screening records
// ============================================================================

export interface SuspiciousIPMarkItem {
  ip: string
  source: string
  context: string
  ban_context: string
  ban_reason: string
  trigger_count: number
  last_triggered_at: number
}

export interface LogScreeningRecordItem {
  id: number
  user_id: number
  username: string
  discord_id: string
  discord_uid: number
  risk_level: string
  observed_until: number
  require_manual_review: boolean
  display_name: string
  remark: string
  token_name: string
  ip: string
  rule_name: string
  window: string
  window_start: number
  window_end: number
  request_count: number
  rpm: number
  rph: number
  tpm: number
  param_hits: string[]
  ua_hits: string[]
  prompt_delta_count: number
  prompt_delta_max: number
  request_path: string
  matched_at: number
  expires_at: number
  created_at: number
  updated_at: number
  operator_user_id: number
  operator_name: string
  manual_triggered: boolean
  suspicious_ips: SuspiciousIPMarkItem[]
}

export interface LogScreeningListParams {
  p?: number
  page_size?: number
  user_id?: number
  username?: string
  ip?: string
  rule?: string
  window?: string
  param_key?: string
  ua?: string
  request_path?: string
  start_timestamp?: number
  end_timestamp?: number
  /** Tristate: omit for "any", "1"/"true" for expired only, "0"/"false" for active. */
  expired?: string
}

// ============================================================================
// Run / cleanup
// ============================================================================

export interface LogScreeningRunSummary {
  kind: string
  status: string
  enabled: boolean
  rules_total: number
  rules_checked: number
  records_created: number
  records_updated: number
  expired: number
  started_at: number
  finished_at: number
  elapsed_ms: number
  window_start: number
  window_end: number
  manual: boolean
  operator_user_id: number
  operator_name: string
  capped: boolean
  candidate_limit: number
  detail_limit: number
  candidates_seen: number
  details_seen: number
}

export interface LogScreeningCleanupResult {
  deleted: number
}

// ============================================================================
// Prompt block logs
// ============================================================================

export interface PromptBlockLogItem {
  id: number
  user_id: number
  username: string
  display_name: string
  remark: string
  ip: string
  rule_pattern: string
  rule_message: string
  error_code: string
  http_status_code: number
  request_path: string
  match_mode: string
  auto_ban_configured: boolean
  auto_banned: boolean
  ban_reason: string
  matched_at: number
  created_at: number
  updated_at: number
}

export interface PromptBlockLogDetail extends PromptBlockLogItem {
  request_headers_raw: string
  request_params_raw: string
}

export interface PromptBlockLogListParams {
  p?: number
  page_size?: number
  user_id?: number
  username?: string
  ip?: string
  rule_pattern?: string
  request_path?: string
  error_code?: string
  match_mode?: string
  auto_banned?: string
  start_timestamp?: number
  end_timestamp?: number
  status_code_min?: number
  status_code_max?: number
}

// ============================================================================
// UA block logs
// ============================================================================

export interface UABlockLogItem {
  id: number
  user_id: number
  username: string
  display_name: string
  remark: string
  ip: string
  user_agent: string
  rule_pattern: string
  rule_message: string
  error_code: string
  http_status_code: number
  request_path: string
  is_empty_ua: boolean
  auto_ban_configured: boolean
  auto_banned: boolean
  ban_reason: string
  matched_at: number
  created_at: number
  updated_at: number
}

export interface UABlockLogDetail extends UABlockLogItem {
  request_headers_raw: string
  request_params_raw: string
}

export interface UABlockLogListParams {
  p?: number
  page_size?: number
  user_id?: number
  username?: string
  ip?: string
  rule_pattern?: string
  request_path?: string
  error_code?: string
  is_empty_ua?: string
  auto_banned?: string
  start_timestamp?: number
  end_timestamp?: number
  status_code_min?: number
  status_code_max?: number
}

// ============================================================================
// Settings (option API keys exposed for log screening)
// ============================================================================

/**
 * Raw string values read from the option API. JSON-stored options are kept as
 * their raw serialized string and edited through a validating textarea.
 * NOTE: CheckSensitiveAutoBanSyncEnabled / AutoBanSync are intentionally not
 * modeled — ban_sync is deprecated for this branch.
 */
export interface LogScreeningOptionValues {
  CheckSensitiveEnabled: boolean
  CheckSensitiveOnPromptEnabled: boolean
  SensitiveWords: string
  SensitivePromptRegexRules: string
  SensitivePromptBlockedMessage: string
  CheckSensitiveOnUAEnabled: boolean
  SensitiveUABlockedRegexes: string
  SensitiveUARegexRules: string
  SensitiveUAGroupRegexRules: string
  SensitiveUABlockedMessage: string
  CheckSensitiveOnEmptyUAEnabled: boolean
  CheckSensitiveOnEmptyUAAutoBanEnabled: boolean
  SensitiveEmptyUABlockedMessage: string
  SensitiveEmptyUABlockedHTTPStatusCode: string
  SensitiveEmptyUABlockedErrorCode: string
  /** JSON string registered via GlobalConfig.Register("log_screening"). */
  log_screening: string
  /** JSON string registered via GlobalConfig.Register("relay_param_record"). */
  relay_param_record: string
}

export type LogScreeningOptionKey = keyof LogScreeningOptionValues
