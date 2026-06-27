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
 * Type definitions for the admin error insight feature.
 */

// ============================================================================
// Common response wrapper
// ============================================================================

export interface ErrorInsightResponse<T> {
  success: boolean
  message?: string
  data?: T
}

// ============================================================================
// Shared filter parameters (subset exposed in the UI; mirrors backend query
// params for /api/error_insight/* endpoints).
// ============================================================================

export interface ErrorInsightFilterParams {
  start_timestamp?: number
  end_timestamp?: number
  rule_matched?: boolean
  rule_code?: string
  unmatched_reason?: string
  model_name?: string
  channel_id?: number
  error_source?: string
  error_stage?: string
  client_status_code?: number
  upstream_status_code?: number
  is_stream?: boolean
  username?: string
  request_id?: string
  normalized_signature?: string
  request_path?: string
}

// ============================================================================
// Summary
// ============================================================================

export interface ErrorInsightSummary {
  total_count: number
  rule_matched_count: number
  unmatched_count: number
  distinct_signatures: number
  affected_users: number
  affected_channels: number
  top_rule_code: string
  top_rule_code_count: number
}

// ============================================================================
// Signatures
// ============================================================================

export interface ErrorInsightSignature {
  normalized_signature: string
  normalized_message: string
  rule_code: string
  unmatched_reason: string
  client_status_code: number
  upstream_status_code: number
  error_source: string
  error_stage: string
  count: number
  affected_users: number
  affected_channels: number
  first_seen_at: number
  latest_at: number
}

// ============================================================================
// Logs
// ============================================================================

export interface ErrorInsightLog {
  id: number
  created_at: number
  request_id: string
  user_id: number
  username: string
  token_name: string
  channel_id: number
  model_name: string
  request_path: string
  is_stream: boolean
  error_source: string
  error_stage: string
  client_status_code: number
  upstream_status_code: number
  rule_code: string
  rule_matched: boolean
  match_source: string
  unmatched_reason: string
  safe_error_code: string
  safe_error_type: string
  safe_error_message: string
  original_error_code: string
  original_error_type: string
  original_error_message: string
  normalized_signature: string
  request_time: number
  retry_count: number
}

export interface ErrorInsightLogsData {
  logs: ErrorInsightLog[]
  total: number
}

// ============================================================================
// Delete signature result
// ============================================================================

export interface ErrorInsightDeleteResult {
  deleted: number
}
