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
export const RELAY_ERROR_DEFAULT_MESSAGES: Record<RelayErrorRuleCode, string> =
  {
    insufficient_user_quota: 'Insufficient account balance.',
    invalid_max_tokens: 'Invalid max_tokens; adjust and retry.',
    max_tokens_requires_stream:
      'Enable stream=true when max_tokens exceeds 4096.',
    invalid_budget_tokens:
      'Invalid reasoning.budget_tokens; adjust within allowed range.',
    invalid_stream_options: 'Invalid stream_options; check and retry.',
    invalid_message_role:
      'Invalid message role; check messages.role and use only system, user, assistant, or tool.',
    invalid_image_url:
      'Image URL unreachable or unsupported; replace and retry.',
    context_length_exceeded:
      'Request exceeds model context or token limit; shorten input or lower max_tokens.',
    content_filtered:
      'Your content or generated output was blocked by upstream safety policy; adjust and retry.',
    model_not_found: 'Requested model not found.',
    no_available_channel:
      'No available channel for this model; please retry later.',
    model_not_permitted:
      'This token is not authorized for the requested model.',
    risk_control_restricted:
      'Account activity restricted; retry later or contact admin.',
    behavior_banned:
      'Account restricted due to abnormal behavior; contact admin.',
    upstream_rate_limited: 'Upstream load is high; please retry later.',
    upstream_timeout: 'Upstream timed out; please retry later.',
    upstream_bad_response:
      'Upstream response format is invalid; retry later, or try enabling streaming if it persists.',
    upstream_unavailable:
      'Upstream temporarily unavailable; please retry later.',
    stream_interrupted: 'Stream interrupted; please retry later.',
    internal_error: 'Internal service error; please retry later.',
  }

/**
 * Human-readable label for each rule code, shown in the admin UI alongside the
 * rule code badge. Ported from nashiyard's governance configuration.
 */
export const RELAY_ERROR_RULE_LABELS: Record<RelayErrorRuleCode, string> = {
  insufficient_user_quota: 'Insufficient user balance',
  invalid_max_tokens: 'Invalid max_tokens',
  max_tokens_requires_stream: 'max_tokens requires stream',
  invalid_budget_tokens: 'Invalid reasoning.budget_tokens',
  invalid_stream_options: 'Invalid stream_options',
  invalid_message_role: 'Invalid message role',
  invalid_image_url: 'Invalid image URL',
  context_length_exceeded: 'Context or token limit exceeded',
  content_filtered: 'Content blocked by safety filter',
  model_not_found: 'Model not found',
  no_available_channel: 'No available channel',
  model_not_permitted: 'Model not permitted for token',
  risk_control_restricted: 'Risk-control restricted',
  behavior_banned: 'Behavior ban',
  upstream_rate_limited: 'Upstream rate-limited',
  upstream_timeout: 'Upstream timeout',
  upstream_bad_response: 'Upstream bad response',
  upstream_unavailable: 'Upstream unavailable',
  stream_interrupted: 'Stream interrupted',
  internal_error: 'Internal error',
}

/**
 * Hint describing when each rule matches, shown as help text in the admin UI.
 * Ported from nashiyard's governance configuration.
 */
export const RELAY_ERROR_RULE_HINTS: Record<RelayErrorRuleCode, string> = {
  insufficient_user_quota: 'Local user quota insufficient',
  invalid_max_tokens:
    'max_tokens invalid, too large/small, or out of allowed range',
  max_tokens_requires_stream:
    'Upstream requires stream=true when max_tokens > 4096',
  invalid_budget_tokens:
    'reasoning.budget_tokens / budget_tokens too large, too small, or incompatible with max_tokens',
  invalid_stream_options: 'stream_options mismatch with stream',
  invalid_message_role: 'messages.role is outside system/user/assistant/tool',
  invalid_image_url: 'Image URL unreachable or unsupported',
  context_length_exceeded:
    'Context limit, token limit, or input/prompt tokens exceeding max_prompt_tokens',
  content_filtered:
    'User input or generated output blocked by safety policy, content review, or upstream filtering',
  model_not_found:
    'Model missing, unavailable, or upstream returned not found/unavailable',
  no_available_channel:
    'No available channel, no channel for capability, or group routing unavailable',
  model_not_permitted:
    'Current token, user group, or model whitelist rejects access',
  risk_control_restricted: 'Risk control restricted',
  behavior_banned: 'Abnormal behavior ban',
  upstream_rate_limited:
    'Explicit upstream rate limit, HTTP 429, or too many requests',
  upstream_timeout: 'Upstream timeout (408/504/524/i/o timeout)',
  upstream_bad_response:
    'Upstream returned non-JSON / SSE data prefix / decode failure',
  upstream_unavailable:
    'Explicit upstream unavailable state, opaque upstream account/credit/billing errors, or openai_error',
  stream_interrupted:
    'Stream interruption, connection closed early, or EOF during upstream response',
  internal_error: 'Local 5xx not matching other rules',
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
  config: RelayErrorGovernanceConfig | null,
  translate?: (key: string) => string
): GovernanceRuleRow[] {
  const rules = config?.rules ?? {}
  return RELAY_ERROR_RULE_CODES.map((code) => {
    const override: RelayErrorRuleOverride | undefined = rules[code]
    const defaultMessage = RELAY_ERROR_DEFAULT_MESSAGES[code] ?? ''
    return {
      code,
      enabled: override?.enabled ?? DEFAULT_RULE_ENABLED,
      message:
        override?.message ?? translate?.(defaultMessage) ?? defaultMessage,
    }
  })
}

/**
 * Serialize editable rows into the governance config shape used by nashiyard:
 * every rule is written with its current enabled state and response message.
 * This makes the message field a real editable value instead of a placeholder.
 */
export function serializeGovernanceConfig(
  enabled: boolean,
  rows: GovernanceRuleRow[],
  translate?: (key: string) => string,
  customRules?: RelayErrorGovernanceConfig['custom_rules']
): RelayErrorGovernanceConfig {
  const rules: Record<string, RelayErrorRuleOverride> = {}
  for (const row of rows) {
    const defaultMessageKey = RELAY_ERROR_DEFAULT_MESSAGES[row.code] ?? ''
    rules[row.code] = {
      enabled: row.enabled,
      message:
        row.message.trim() ||
        translate?.(defaultMessageKey) ||
        defaultMessageKey,
    }
  }
  return { enabled, rules, custom_rules: customRules ?? [] }
}
