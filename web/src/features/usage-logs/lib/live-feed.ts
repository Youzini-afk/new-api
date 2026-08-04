/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import type { UsageLog } from '../data/schema'

const LIVE_FEED_TARGET_BATCH_DURATION_MS = 1_400
const LIVE_FEED_MIN_INSERT_INTERVAL_MS = 55
const LIVE_FEED_MAX_INSERT_INTERVAL_MS = 420

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
): { items: UsageLog[]; newItems: UsageLog[]; newKeys: string[] } {
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
    return { items: previous, newItems: [], newKeys: [] }
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
    newItems,
    newKeys: newItems.map(getUsageLogLiveKey),
  }
}

export function getLiveFeedInsertInterval(itemCount: number): number {
  if (itemCount <= 1) return 0

  return Math.min(
    LIVE_FEED_MAX_INSERT_INTERVAL_MS,
    Math.max(
      LIVE_FEED_MIN_INSERT_INTERVAL_MS,
      Math.round(LIVE_FEED_TARGET_BATCH_DURATION_MS / (itemCount - 1))
    )
  )
}

/** Insert oldest-first so the final list remains newest-first. */
export function getLiveFeedInsertionOrder(newItems: UsageLog[]): UsageLog[] {
  return [...newItems].reverse()
}
