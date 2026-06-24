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
 * Utility functions for operations stats feature
 */

/**
 * Get Unix timestamps (seconds) for a relative time range ending now.
 */
export function getTimeRangeFromDays(days: number): {
  start_timestamp: number
  end_timestamp: number
} {
  const now = Math.floor(Date.now() / 1000)
  return {
    end_timestamp: now,
    start_timestamp: now - days * 24 * 60 * 60,
  }
}

/**
 * Build API time range parameters. Falls back to range_days when explicit
 * timestamps are not provided, matching backend behavior.
 */
export function buildTimeRangeParams(
  rangeDays: number,
  startTimestamp?: number,
  endTimestamp?: number
): {
  range_days: number
  start_timestamp?: number
  end_timestamp?: number
} {
  if (startTimestamp && endTimestamp) {
    return {
      range_days: rangeDays,
      start_timestamp: startTimestamp,
      end_timestamp: endTimestamp,
    }
  }
  return { range_days: rangeDays }
}

/**
 * Convert a Date object to Unix timestamp in seconds.
 */
export function dateToTimestampSeconds(date: Date): number {
  return Math.floor(date.getTime() / 1000)
}

/**
 * Format a Unix timestamp (seconds) for display.
 */
export function formatTimestamp(
  timestamp: number,
  options?: Intl.DateTimeFormatOptions
): string {
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

/**
 * Strip optional sk- prefix from an API key.
 */
export function normalizeLookupKey(key: string): string {
  const trimmed = key.trim()
  if (trimmed.toLowerCase().startsWith('sk-')) {
    return trimmed.slice(3)
  }
  return trimmed
}
