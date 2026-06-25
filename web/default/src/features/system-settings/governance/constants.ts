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
import type {
  GovernanceRuleRow,
  RelayErrorGovernanceConfig,
  RelayErrorRuleCode,
  RelayErrorRuleOverride,
} from './types'

/**
 * The fixed set of 20 relay error rule codes, in the canonical display order.
 *
 * These mirror the backend governance rule registry and must not be added to
 * or removed from — the backend owns the full rule set (status / type / code /
 * param) and only `enabled` + `message` may be overridden from the UI.
 */
export const RELAY_ERROR_RULE_CODES = [
  'insufficient_user_quota',
  'invalid_max_tokens',
  'max_tokens_requires_stream',
  'invalid_budget_tokens',
  'invalid_stream_options',
  'invalid_message_role',
  'invalid_image_url',
  'context_length_exceeded',
  'content_filtered',
  'model_not_found',
  'no_available_channel',
  'model_not_permitted',
  'risk_control_restricted',
  'behavior_banned',
  'upstream_rate_limited',
  'upstream_timeout',
  'upstream_bad_response',
  'upstream_unavailable',
  'stream_interrupted',
  'internal_error',
] as const satisfies readonly RelayErrorRuleCode[]

/**
 * Default user-facing message shown for each rule code when no custom override
 * is configured. These double as the placeholder for the custom message input.
 *
 * Kept in sync with the backend defaults so admins can see what users would
 * receive by default before deciding to override.
 */
export const RELAY_ERROR_DEFAULT_MESSAGES: Record<
  RelayErrorRuleCode,
  string
> = {
  insufficient_user_quota:
    'Your quota is insufficient to complete this request.',
  invalid_max_tokens: 'The max_tokens parameter is invalid.',
  max_tokens_requires_stream:
    'max_tokens can only be set when streaming is enabled.',
  invalid_budget_tokens: 'The budget tokens parameter is invalid.',
  invalid_stream_options: 'The stream options provided are invalid.',
  invalid_message_role: 'The message role is not supported.',
  invalid_image_url: 'The image URL is invalid or unreachable.',
  context_length_exceeded:
    "This request exceeds the model's maximum context length.",
  content_filtered: 'Your request was filtered by the content safety policy.',
  model_not_found: 'The requested model does not exist.',
  no_available_channel:
    'No available channel can serve this model right now.',
  model_not_permitted: 'You do not have permission to use this model.',
  risk_control_restricted: 'This request was blocked by risk control.',
  behavior_banned: 'Your account behavior has been restricted.',
  upstream_rate_limited:
    'The upstream service is rate-limiting requests.',
  upstream_timeout: 'The upstream service took too long to respond.',
  upstream_bad_response:
    'The upstream service returned an invalid response.',
  upstream_unavailable: 'The upstream service is temporarily unavailable.',
  stream_interrupted: 'The response stream was interrupted.',
  internal_error:
    'An error occurred while processing the request. Please try again later.',
}

/** The system option key under which the governance config is stored. */
export const RELAY_ERROR_GOVERNANCE_OPTION_KEY = 'relay_error_governance'

/** Default per-rule `enabled` state when no override is stored. */
export const DEFAULT_RULE_ENABLED = true

/**
 * Build the editable row list from a parsed governance config, merging stored
 * overrides onto the default state for each rule code.
 */
export function buildGovernanceRows(
  config: RelayErrorGovernanceConfig | null
): GovernanceRuleRow[] {
  const rules = config?.rules ?? {}
  return RELAY_ERROR_RULE_CODES.map((code) => {
    const override: RelayErrorRuleOverride | undefined = rules[code]
    return {
      code,
      enabled: override?.enabled ?? DEFAULT_RULE_ENABLED,
      message: override?.message ?? '',
    }
  })
}

/**
 * Serialize the editable rows back into the sparse governance config shape.
 *
 * Only rules that deviate from the default (enabled === true, empty message)
 * are emitted as overrides, matching the backend's "optional per-rule
 * override" contract.
 */
export function serializeGovernanceConfig(
  enabled: boolean,
  rows: GovernanceRuleRow[]
): RelayErrorGovernanceConfig {
  const rules: Record<string, RelayErrorRuleOverride> = {}
  for (const row of rows) {
    const override: RelayErrorRuleOverride = {}
    if (row.enabled !== DEFAULT_RULE_ENABLED) {
      override.enabled = row.enabled
    }
    if (row.message.trim() !== '') {
      override.message = row.message.trim()
    }
    if (Object.keys(override).length > 0) {
      rules[row.code] = override
    }
  }
  return { enabled, rules }
}
