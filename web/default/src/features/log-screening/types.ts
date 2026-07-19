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
  user_agent?: string
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

// ============================================================================
// Comprehensive risk control
// ============================================================================

export type RiskCaseStatus =
  | 'open'
  | 'reviewing'
  | 'actioned'
  | 'resolved'
  | 'dismissed'

export type RiskVerdict =
  | 'normal'
  | 'small_share'
  | 'key_leak'
  | 'gateway_distribution'
  | 'multi_node_gateway'
  | 'commercial_resale'
  | 'forbidden_paid_client'
  | 'uncertain'

export type RiskActionType =
  | 'none'
  | 'observe'
  | 'rate_limit'
  | 'freeze_token'
  | 'temporary_block'
  | 'permanent_ban'
  | 'manual_review'
  | 'clear'

export interface RiskEvidenceSample {
  request_id: string
  created_at: number
  ip: string
  user_agent: string
  model: string
  request_path: string
  request_params?: string
}

export interface RiskSignalSummary {
  request_count: number
  total_tokens: number
  total_quota: number
  distinct_tokens: number
  error_count: number
  error_rate: number
  max_rpm: number
  average_rpm: number
  max_concurrency: number
  distinct_ips: number
  distinct_uas: number
  distinct_models: number
  distinct_paths: number
  active_hours: number
  distinct_semantics: number
  dominant_semantic_ratio: number
  gateway_ua_hits: number
  forbidden_client_ua_hits: number
  top_ip: string
  top_ua: string
  detail_rows: number
  detail_sampled: boolean
  samples: RiskEvidenceSample[]
}

export interface RiskAgentEvidence {
  signal_id: string
  strength: number
  summary: string
  request_ids: string[]
}

export interface RiskSuggestedFingerprint {
  kind: 'none' | 'ua' | 'prompt' | 'tool_schema' | 'header' | 'combined' | ''
  pattern: string
  reason: string
}

export interface RiskAgentDecision {
  verdict: RiskVerdict
  risk_score: number
  confidence: number
  agrees_with_triage?: boolean
  policy_violation: boolean
  evidence: RiskAgentEvidence[]
  counter_evidence: string[]
  recommended_action: RiskActionType
  recommended_duration_minutes: number
  admin_reason: string
  user_reason: string
  suggested_fingerprint: RiskSuggestedFingerprint
}

export interface RiskCaseItem {
  id: number
  fingerprint: string
  user_id: number
  username: string
  token_id: number
  token_name: string
  status: RiskCaseStatus
  verdict: RiskVerdict
  rule_verdict: RiskVerdict
  risk_level: string
  rule_score: number
  agent_score: number
  judge_score: number
  final_score: number
  confidence: number
  policy_violation: boolean
  signals: string
  sample_request_ids: string
  rule_reason: string
  agent_result: string
  judge_result: string
  agent_model: string
  judge_model: string
  agent_analyzed_at: number
  judge_analyzed_at: number
  rule_recommended_action: RiskActionType
  rule_recommended_duration_minutes: number
  recommended_action: RiskActionType
  recommended_duration_minutes: number
  recommended_reason: string
  recommended_user_reason: string
  window_hours: number
  window_start: number
  window_end: number
  repeat_count: number
  action_id: number
  reviewed_by: number
  reviewed_by_name: string
  review_note: string
  reviewed_at: number
  last_seen_at: number
  created_at: number
  updated_at: number
}

export interface RiskActionItem {
  id: number
  case_id: number
  user_id: number
  token_id: number
  action: RiskActionType
  source: string
  parameters: string
  reason: string
  user_message: string
  started_at: number
  expires_at: number
  status: string
  operator_user_id: number
  operator_name: string
  created_at: number
  updated_at: number
}

export interface RiskCaseDetail {
  case: RiskCaseItem
  signals: RiskSignalSummary | null
  agent_result: RiskAgentDecision | null
  judge_result: RiskAgentDecision | null
  actions: RiskActionItem[]
}

export interface RiskCaseListParams {
  p?: number
  page_size?: number
  user_id?: number
  token_id?: number
  status?: string
  verdict?: string
  risk_level?: string
  min_score?: number
  start_timestamp?: number
  end_timestamp?: number
}

export interface RiskActionRequest {
  action: RiskActionType
  duration_minutes: number
  request_limit: number
  reason: string
  user_message: string
}

export interface RiskControlSetting {
  enabled: boolean
  schedule_enabled: boolean
  interval_minutes: number
  window_hours: number[]
  candidate_limit: number
  detail_limit: number
  max_samples: number
  min_requests: number
  case_threshold: number
  high_rpm: number
  critical_rpm: number
  ip_fanout_threshold: number
  ua_fanout_threshold: number
  concurrency_threshold: number
  active_hours_threshold: number
  gateway_ua_markers: string[]
  forbidden_client_ua_markers: string[]
  case_cooldown_minutes: number
  include_request_content: boolean
  redact_sensitive: boolean
  agent_enabled: boolean
  channel_id: number
  triage_model: string
  judge_model: string
  agent_min_rule_score: number
  max_agent_cases_per_run: number
  agent_concurrency: number
  agent_retry_count: number
  judge_min_final_score: number
  triage_prompt_template: string
  judge_prompt_template: string
  json_output_params: unknown
  auto_action_enabled: boolean
  auto_rate_limit_enabled: boolean
  auto_freeze_token_enabled: boolean
  auto_temp_block_enabled: boolean
  auto_permanent_ban_enabled: boolean
  auto_action_min_score: number
  auto_permanent_min_score: number
  auto_action_min_confidence: number
  rate_limit_per_minute: number
  temporary_block_minutes: number
  max_auto_actions_per_run: number
}

export interface RiskRunTaskResult {
  created: boolean
  task: {
    task_id: string
    type: string
    status: string
    created_at: number
  }
}
