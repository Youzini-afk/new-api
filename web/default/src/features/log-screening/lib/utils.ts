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
 * Utility helpers for log screening display.
 */

/** Format a Unix timestamp (seconds) for display. */
export function formatScreeningTimestamp(
  timestamp: number,
  options?: Intl.DateTimeFormatOptions
): string {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    ...options,
  })
}

/** Compact number formatting for counts. */
export function formatScreeningNumber(value: number): string {
  if (value === undefined || value === null || Number.isNaN(value)) return '-'
  return value.toLocaleString()
}

/**
 * Pretty-print a JSON string for display in a textarea/preview. Falls back to
 * the raw input when parsing fails (callers should validate separately).
 */
export function prettyJson(raw: string): string {
  const trimmed = (raw || '').trim()
  if (!trimmed) return ''
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2)
  } catch {
    return raw
  }
}

/** Validate that a string parses as JSON. Returns the error message or null. */
export function validateJson(raw: string): string | null {
  const trimmed = (raw || '').trim()
  if (!trimmed) return null
  try {
    JSON.parse(trimmed)
    return null
  } catch (e) {
    return e instanceof Error ? e.message : String(e)
  }
}

/**
 * Attempt to detect whether a raw option string looks like a JSON array/object
 * (so the UI can pick the right editor affordance).
 */
export function looksLikeJson(raw: string): boolean {
  const trimmed = (raw || '').trim()
  return trimmed.startsWith('{') || trimmed.startsWith('[')
}
