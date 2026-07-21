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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  compileGoRegexForConflictPreview,
  getCustomRuleConflicts,
} from './error-governance-conflicts.ts'
import type { RelayErrorCustomRule } from './types'

function rule(overrides: Partial<RelayErrorCustomRule>): RelayErrorCustomRule {
  return {
    enabled: true,
    rule_code: 'rule',
    match_type: 'contains',
    match_pattern: 'pattern',
    safe_error_code: 'safe_error',
    safe_error_type: 'invalid_request_error',
    safe_error_message: 'Safe error.',
    status_code: 400,
    ...overrides,
  }
}

describe('error governance conflict preview', () => {
  test('supports leading Go case-insensitive flags', () => {
    const regex = compileGoRegexForConflictPreview(
      '(?i)UA_BLOCKED_CODING_AGENT'
    )
    assert.ok(regex)
    assert.equal(regex.test('ua_blocked_coding_agent'), true)
  })

  test('does not spread unsupported regex previews to unrelated rules', () => {
    const conflicts = getCustomRuleConflicts([
      rule({ rule_code: 'script', match_pattern: 'ua_blocked_script_client' }),
      rule({
        rule_code: 'coding',
        match_type: 'regex',
        match_pattern: '(?i:ua_blocked_coding_agent)',
      }),
      rule({ rule_code: 'content', match_pattern: 'content_policy_violation' }),
    ])

    assert.deepEqual(conflicts, [[], [], []])
  })

  test('still reports genuine contains and translated regex overlaps', () => {
    const conflicts = getCustomRuleConflicts([
      rule({ rule_code: 'rate', match_pattern: 'rate limit' }),
      rule({ rule_code: 'rate_long', match_pattern: 'rate limit exceeded' }),
      rule({
        rule_code: 'not_found_regex',
        match_type: 'regex',
        match_pattern: '(?i)^not found$',
      }),
      rule({ rule_code: 'not_found', match_pattern: 'not found' }),
    ])

    assert.deepEqual(conflicts[0], ['overlap: rate_long'])
    assert.deepEqual(conflicts[1], ['overlap: rate'])
    assert.deepEqual(conflicts[2], ['regex overlaps: not_found'])
    assert.deepEqual(conflicts[3], ['covered by: not_found_regex'])
  })
})
