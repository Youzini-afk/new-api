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
import type { UsageLog } from '../data/schema'

/**
 * ClickHouse supplies page-relative display IDs, so the live feed cannot use
 * `id` to recognize a row across polls. Request IDs are the best identity when
 * available; the fallback combines immutable log fields for local log types.
 */
export function getUsageLogLiveKey(log: UsageLog): string {
  if (log.request_id) {
    return `request:${JSON.stringify([
      log.request_id,
      log.type,
      log.created_at,
    ])}`
  }

  return `log:${JSON.stringify([
    log.created_at,
    log.type,
    log.user_id,
    log.token_id,
    log.channel,
    log.model_name,
    log.prompt_tokens,
    log.completion_tokens,
    log.quota,
    log.use_time,
    log.is_stream,
    log.upstream_request_id,
    hashText(log.content),
    hashText(log.other),
  ])}`
}

function hashText(value: string): string {
  let hash = 2166136261
  for (let index = 0; index < value.length; index += 1) {
    hash = Math.imul(hash ^ value.charCodeAt(index), 16777619)
  }
  return (hash >>> 0).toString(36)
}

export function mergeUsageLogLiveFeed(
  previous: UsageLog[],
  incoming: UsageLog[],
  limit: number
): { items: UsageLog[]; newKeys: string[] } {
  const previousKeys = new Set(previous.map(getUsageLogLiveKey))
  const nextKeys = new Set<string>()
  const newItems: UsageLog[] = []

  for (const log of incoming) {
    const key = getUsageLogLiveKey(log)
    if (!previousKeys.has(key) && !nextKeys.has(key)) {
      nextKeys.add(key)
      newItems.push(log)
    }
  }

  if (newItems.length === 0) {
    return { items: previous, newKeys: [] }
  }

  const merged: UsageLog[] = []
  const mergedKeys = new Set<string>()

  for (const log of [...newItems, ...previous]) {
    const key = getUsageLogLiveKey(log)
    if (!mergedKeys.has(key)) {
      mergedKeys.add(key)
      merged.push(log)
    }
  }

  return {
    items: merged.slice(0, Math.max(0, limit)),
    newKeys: newItems.map(getUsageLogLiveKey),
  }
}
