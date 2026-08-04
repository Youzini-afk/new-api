import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { UsageLog } from '../data/schema'
import {
  getLiveFeedInsertInterval,
  getLiveFeedInsertionOrder,
  getUsageLogLiveKey,
  mergeUsageLogLiveFeed,
} from './live-feed'

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
    ...overrides,
  }
}

describe('usage log live feed', () => {
  test('keeps identity stable when ClickHouse display IDs change', () => {
    assert.equal(
      getUsageLogLiveKey(createLog({ id: 1 })),
      getUsageLogLiveKey(createLog({ id: 99 }))
    )
  })

  test('keeps fallback identity stable and bounded', () => {
    const first = createLog({ id: 1, request_id: '', content: 'manage' })
    const next = createLog({ id: 88, request_id: '', content: 'manage' })
    assert.equal(getUsageLogLiveKey(first), getUsageLogLiveKey(next))
    assert.ok(
      getUsageLogLiveKey(
        createLog({ request_id: '', content: 'prompt'.repeat(10_000) })
      ).length < 256
    )
  })

  test('prepends unseen rows in upstream order and trims the oldest', () => {
    const log2 = createLog({ created_at: 102, request_id: 'request-2' })
    const log3 = createLog({ created_at: 103, request_id: 'request-3' })
    const log4 = createLog({ created_at: 104, request_id: 'request-4' })
    const result = mergeUsageLogLiveFeed([log2], [log4, log3, log2], 2)

    assert.deepEqual(
      result.items.map((log) => log.request_id),
      ['request-4', 'request-3']
    )
    assert.deepEqual(result.newKeys, [
      getUsageLogLiveKey(log4),
      getUsageLogLiveKey(log3),
    ])
  })

  test('uses a bounded adaptive insertion interval', () => {
    assert.equal(getLiveFeedInsertInterval(1), 0)
    assert.equal(getLiveFeedInsertInterval(4), 420)
    assert.equal(getLiveFeedInsertInterval(100), 55)
  })

  test('inserts newest-first batches oldest-first', () => {
    const rows = ['request-3', 'request-2', 'request-1'].map((request_id) =>
      createLog({ request_id })
    )
    assert.deepEqual(
      getLiveFeedInsertionOrder(rows).map((log) => log.request_id),
      ['request-1', 'request-2', 'request-3']
    )
  })
})
