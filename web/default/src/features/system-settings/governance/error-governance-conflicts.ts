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
import type { RelayErrorCustomRule } from './types'

function comparablePattern(rule: RelayErrorCustomRule) {
  const pattern = (rule.match_pattern || '').trim()
  return rule.match_type === 'contains' ? pattern.toLowerCase() : pattern
}

// Conflict detection is only a browser-side preview. The backend's Go regexp
// compiler remains authoritative. Translate the common leading RE2 flags so a
// valid pattern such as `(?i)foo` is not incorrectly marked invalid by JS.
export function compileGoRegexForConflictPreview(pattern: string) {
  let source = pattern.trim()
  const flags = new Set<string>()

  while (true) {
    const inlineFlags = source.match(/^\(\?([ims]+)\)/)
    if (!inlineFlags) break
    for (const flag of inlineFlags[1]) flags.add(flag)
    source = source.slice(inlineFlags[0].length)
  }

  // These RE2 constructs either have different JavaScript semantics or are not
  // supported by JavaScript. Skip heuristic overlap checks instead of emitting
  // a false conflict; strict validation still happens on the server.
  if (
    /\(\?[imsU-]+:/.test(source) ||
    /\[\[:/.test(source) ||
    /\\[ACz]/.test(source)
  ) {
    return null
  }

  source = source.replaceAll(/\(\?P<([A-Za-z_][A-Za-z0-9_]*)>/g, '(?<$1>')
  try {
    return new RegExp(source, [...flags].join(''))
  } catch {
    return null
  }
}

export function getCustomRuleConflicts(rules: RelayErrorCustomRule[]) {
  const patterns = rules.map(comparablePattern)
  const regexes = rules.map((rule) =>
    rule.match_type === 'regex'
      ? compileGoRegexForConflictPreview(rule.match_pattern || '')
      : null
  )

  return rules.map((rule, index) => {
    const conflicts: string[] = []
    const pattern = patterns[index]
    for (let i = 0; i < rules.length; i += 1) {
      if (i === index) continue
      const other = rules[i]
      const otherPattern = patterns[i]
      if (!pattern || !otherPattern) continue
      if (rule.rule_code && rule.rule_code === other.rule_code) {
        conflicts.push(`duplicate code: ${other.rule_code}`)
        continue
      }
      if (rule.match_type === other.match_type && pattern === otherPattern) {
        conflicts.push(`same pattern: ${other.rule_code}`)
        continue
      }
      if (rule.match_type === 'contains' && other.match_type === 'contains') {
        if (pattern.includes(otherPattern) || otherPattern.includes(pattern)) {
          conflicts.push(`overlap: ${other.rule_code}`)
        }
        continue
      }
      if (rule.match_type === 'regex' && regexes[index]?.test(otherPattern)) {
        conflicts.push(`regex overlaps: ${other.rule_code}`)
      }
      if (other.match_type === 'regex' && regexes[i]?.test(pattern)) {
        conflicts.push(`covered by: ${other.rule_code}`)
      }
    }
    return [...new Set(conflicts)]
  })
}
