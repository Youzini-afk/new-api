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
 * Relay Error Governance rule configuration types.
 *
 * The full rule set (status / type / code / param) is owned by the backend and
 * is intentionally NOT editable from the UI. Only the per-rule `enabled` flag
 * and `message` text may be overridden.
 */

/** A single relay error rule code. The set is fixed and cannot be extended. */
export type RelayErrorRuleCode =
  | 'insufficient_user_quota'
  | 'invalid_max_tokens'
  | 'max_tokens_requires_stream'
  | 'invalid_budget_tokens'
  | 'invalid_stream_options'
  | 'invalid_message_role'
  | 'invalid_image_url'
  | 'context_length_exceeded'
  | 'content_filtered'
  | 'model_not_found'
  | 'no_available_channel'
  | 'model_not_permitted'
  | 'risk_control_restricted'
  | 'behavior_banned'
  | 'upstream_rate_limited'
  | 'upstream_timeout'
  | 'upstream_bad_response'
  | 'upstream_unavailable'
  | 'stream_interrupted'
  | 'internal_error'

/** Per-rule override. Both fields are optional — absent means "use default". */
export type RelayErrorRuleOverride = {
  enabled?: boolean
  message?: string
}

/** Shape of the `relay_error_governance` system option (stored as JSON string). */
export type RelayErrorGovernanceConfig = {
  enabled: boolean
  rules: Record<string, RelayErrorRuleOverride>
}

/** Editable row state used by the governance section form. */
export type GovernanceRuleRow = {
  code: RelayErrorRuleCode
  /** Whether governance handling is active for this rule. */
  enabled: boolean
  /** Custom override message. Empty string means "use the default message". */
  message: string
}
