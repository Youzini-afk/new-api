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

import type { Channel } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../channel-form'

function validForm() {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'limited upstream',
    key: 'test-key',
    models: 'gpt-5',
  }
}

describe('channel traffic control form', () => {
  test('requires at least one active limit and a timeout for a queue', () => {
    const withoutLimit = channelFormSchema.safeParse({
      ...validForm(),
      traffic_control_enabled: true,
      traffic_max_concurrency: 0,
      traffic_rpm: 0,
    })
    assert.equal(withoutLimit.success, false)

    const withoutTimeout = channelFormSchema.safeParse({
      ...validForm(),
      traffic_control_enabled: true,
      traffic_max_concurrency: 2,
      traffic_queue_size: 10,
      traffic_queue_timeout_seconds: 0,
    })
    assert.equal(withoutTimeout.success, false)

    assert.equal(
      channelFormSchema.safeParse({
        ...validForm(),
        traffic_control_enabled: true,
        traffic_max_concurrency: 2,
        traffic_rpm: 30,
        traffic_queue_size: 10,
        traffic_queue_timeout_seconds: 20,
      }).success,
      true
    )
  })

  test('serializes limits without discarding unrelated channel settings', () => {
    const payload = transformFormDataToCreatePayload({
      ...validForm(),
      settings: JSON.stringify({ custom_value: 'preserved' }),
      traffic_control_enabled: true,
      traffic_max_concurrency: 3,
      traffic_rpm: 45,
      traffic_queue_size: 80,
      traffic_queue_timeout_seconds: 25,
    })
    const settings = JSON.parse(String(payload.channel.settings))

    assert.equal(settings.custom_value, 'preserved')
    assert.deepEqual(settings.traffic_control, {
      enabled: true,
      max_concurrency: 3,
      rpm: 45,
      queue_size: 80,
      queue_timeout_seconds: 25,
    })
  })

  test('loads an existing traffic-control configuration', () => {
    const channel = {
      id: 7,
      type: 1,
      key: '',
      status: 1,
      name: 'limited upstream',
      created_time: 0,
      test_time: 0,
      response_time: 0,
      other: '',
      balance: 0,
      balance_updated_time: 0,
      models: 'gpt-5',
      group: 'default',
      used_quota: 0,
      other_info: '',
      remark: '',
      max_input_tokens: 0,
      channel_info: {
        is_multi_key: false,
        multi_key_size: 0,
        multi_key_polling_index: 0,
        multi_key_mode: 'random',
      },
      settings: JSON.stringify({
        traffic_control: {
          enabled: true,
          max_concurrency: 4,
          rpm: 60,
          queue_size: 120,
          queue_timeout_seconds: 35,
        },
      }),
    } as Channel

    const form = transformChannelToFormDefaults(channel)
    assert.equal(form.traffic_control_enabled, true)
    assert.equal(form.traffic_max_concurrency, 4)
    assert.equal(form.traffic_rpm, 60)
    assert.equal(form.traffic_queue_size, 120)
    assert.equal(form.traffic_queue_timeout_seconds, 35)
  })
})
