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

import type { UsageLog } from '../data/schema'
import { getUsageLogLiveKey, mergeUsageLogLiveFeed } from './live-feed'

function createLog(overrides: Partial<UsageLog> = {}): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 100,
    type: 2,
    content: '',
    username: '',
    token_name: '',
    model_name: 'test-model',
    quota: 1,
    prompt_tokens: 1,
    completion_tokens: 1,
    use_time: 1,
    is_stream: false,
    channel: 1,
    channel_name: '',
    token_id: 1,
    group: 'default',
    ip: '',
    other: '',
    request_id: 'request-1',
    upstream_request_id: '',
    user_agent: '',
    avatar_url: '',
    avatar_source: '',
    discord_username: '',
    discord_global_name: '',
    ...overrides,
  }
}

describe('usage log live feed', () => {
  test('keeps the identity stable when ClickHouse display IDs change', () => {
    const first = createLog({ id: 1 })
    const nextPoll = createLog({ id: 99 })

    assert.equal(getUsageLogLiveKey(first), getUsageLogLiveKey(nextPoll))
  })

  test('keeps local logs without request IDs stable across polls', () => {
    const first = createLog({ id: 1, request_id: '', content: 'manage' })
    const nextPoll = createLog({ id: 88, request_id: '', content: 'manage' })

    assert.equal(getUsageLogLiveKey(first), getUsageLogLiveKey(nextPoll))
  })

  test('keeps fallback keys bounded when content auditing stores long text', () => {
    const key = getUsageLogLiveKey(
      createLog({ request_id: '', content: 'prompt'.repeat(10_000) })
    )

    assert.ok(key.length < 256)
  })

  test('prepends only unseen logs in upstream order', () => {
    const log2 = createLog({ created_at: 102, request_id: 'request-2' })
    const log3 = createLog({ created_at: 103, request_id: 'request-3' })
    const log4 = createLog({ created_at: 104, request_id: 'request-4' })

    const result = mergeUsageLogLiveFeed([log2], [log4, log3, log2], 100)

    assert.deepEqual(
      result.items.map((log) => log.request_id),
      ['request-4', 'request-3', 'request-2']
    )
    assert.deepEqual(result.newKeys, [
      getUsageLogLiveKey(log4),
      getUsageLogLiveKey(log3),
    ])
  })

  test('trims the oldest rows to the current page size', () => {
    const previous = [
      createLog({ created_at: 103, request_id: 'request-3' }),
      createLog({ created_at: 102, request_id: 'request-2' }),
      createLog({ created_at: 101, request_id: 'request-1' }),
    ]
    const newest = createLog({ created_at: 104, request_id: 'request-4' })

    const result = mergeUsageLogLiveFeed(previous, [newest], 3)

    assert.deepEqual(
      result.items.map((log) => log.request_id),
      ['request-4', 'request-3', 'request-2']
    )
  })
})
