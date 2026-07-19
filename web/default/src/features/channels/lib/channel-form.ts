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
import { z } from 'zod'

import {
  CHANNEL_STATUS,
  ERROR_MESSAGES,
  MODEL_FETCHABLE_TYPES,
} from '../constants'
import type { Channel } from '../types'
import {
  CHANNEL_TYPE_ADVANCED_CUSTOM,
  advancedCustomConfigUsesRelativeUpstreamPath,
  parseAdvancedCustomConfig,
  stringifyAdvancedCustomConfig,
  validateAdvancedCustomConfig,
} from './advanced-custom'

// ============================================================================
// Form Validation Schema
// ============================================================================

function parseOptionalJson(value: string | undefined): unknown {
  if (!value?.trim()) return undefined
  return JSON.parse(value)
}

function isJsonObjectValue(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isOptionalJsonObject(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    return parsed === undefined || isJsonObjectValue(parsed)
  } catch {
    return false
  }
}

function isOptionalModelMapping(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (!isJsonObjectValue(parsed)) return false
    return Object.values(parsed).every((item) => typeof item === 'string')
  } catch {
    return false
  }
}

function isOptionalStatusCodeMapping(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (!isJsonObjectValue(parsed)) return false
    return Object.entries(parsed).every(([from, to]) => {
      const fromCode = Number(from)
      const toCode = Number(to)
      return (
        Number.isInteger(fromCode) &&
        Number.isInteger(toCode) &&
        fromCode >= 100 &&
        fromCode <= 599 &&
        toCode >= 100 &&
        toCode <= 599
      )
    })
  } catch {
    return false
  }
}

function isCodexCredential(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    return (
      isJsonObjectValue(parsed) &&
      typeof parsed.access_token === 'string' &&
      parsed.access_token.trim().length > 0 &&
      typeof parsed.account_id === 'string' &&
      parsed.account_id.trim().length > 0
    )
  } catch {
    return false
  }
}

function isVertexJsonKey(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (Array.isArray(parsed)) {
      return parsed.every((item) => isJsonObjectValue(item))
    }
    return isJsonObjectValue(parsed)
  } catch {
    return false
  }
}

function addRequiredIssue(
  ctx: z.RefinementCtx,
  path: string,
  message: string
): void {
  ctx.addIssue({
    code: z.ZodIssueCode.custom,
    path: [path],
    message,
  })
}

const AVAILABILITY_TIME_PATTERN = /^(?:[01]\d|2[0-3]):[0-5]\d$/
const DEFAULT_AVAILABILITY_WINDOWS = [
  {
    weekdays: [1, 2, 3, 4, 5, 6, 7],
    start: '00:00',
    end: '08:00',
  },
]
const MAX_CHANNEL_TRAFFIC_CONCURRENCY = 100_000
const MAX_CHANNEL_TRAFFIC_RPM = 10_000_000
const MAX_CHANNEL_TRAFFIC_QUEUE_SIZE = 10_000
const MAX_CHANNEL_TRAFFIC_QUEUE_TIMEOUT_SECONDS = 3_600
const DEFAULT_CHANNEL_TRAFFIC_QUEUE_SIZE = 100
const DEFAULT_CHANNEL_TRAFFIC_QUEUE_TIMEOUT_SECONDS = 30

function normalizeNonNegativeInteger(
  value: unknown,
  fallback: number,
  maximum: number
): number {
  const normalized = Number(value)
  if (!Number.isInteger(normalized) || normalized < 0 || normalized > maximum) {
    return fallback
  }
  return normalized
}

export const channelFormSchema = z
  .object({
    name: z.string().min(1, ERROR_MESSAGES.REQUIRED_NAME),
    type: z.number().min(0, ERROR_MESSAGES.REQUIRED_TYPE),
    base_url: z.string().optional(),
    key: z.string(),
    openai_organization: z.string().optional(),
    models: z.string().min(1, ERROR_MESSAGES.REQUIRED_MODELS),
    group: z.array(z.string()).min(1, ERROR_MESSAGES.REQUIRED_GROUP),
    model_mapping: z
      .string()
      .optional()
      .refine(
        isOptionalModelMapping,
        'Model mapping must be a JSON object with string values'
      ),
    priority: z.number().optional(),
    weight: z.number().optional(),
    test_model: z.string().optional(),
    auto_ban: z.number().optional(),
    status: z.number(),
    status_code_mapping: z
      .string()
      .optional()
      .refine(
        isOptionalStatusCodeMapping,
        'Status code mapping must use valid HTTP status codes'
      ),
    tag: z.string().optional(),
    remark: z
      .string()
      .max(255, 'Remark must be less than 255 characters')
      .optional(),
    setting: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    param_override: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    header_override: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    settings: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    advanced_custom: z.string().optional(),
    other: z.string().optional(),
    // Multi-key options (not sent to backend directly)
    multi_key_mode: z.enum(['single', 'batch', 'multi_to_single']).optional(),
    multi_key_type: z.enum(['random', 'polling']).optional(),
    batch_add_set_key_prefix_2_name: z.boolean().optional(),
    key_mode: z.enum(['append', 'replace']).optional(), // For editing multi-key channels
    // Channel extra settings (stored in setting JSON, not sent directly)
    force_format: z.boolean().optional(),
    thinking_to_content: z.boolean().optional(),
    proxy: z.string().optional(),
    pass_through_body_enabled: z.boolean().optional(),
    system_prompt: z.string().optional(),
    system_prompt_override: z.boolean().optional(),
    // Type-specific settings (stored in settings JSON)
    is_enterprise_account: z.boolean().optional(), // OpenRouter specific
    vertex_key_type: z.enum(['json', 'api_key']).optional(), // Vertex AI specific
    aws_key_type: z.enum(['ak_sk', 'api_key']).optional(), // AWS specific
    azure_responses_version: z.string().optional(), // Azure specific
    // Field passthrough controls (stored in settings JSON)
    allow_service_tier: z.boolean().optional(), // OpenAI/Anthropic
    disable_store: z.boolean().optional(), // OpenAI only
    allow_safety_identifier: z.boolean().optional(), // OpenAI only
    allow_include_obfuscation: z.boolean().optional(), // OpenAI: include usage obfuscation
    allow_inference_geo: z.boolean().optional(), // OpenAI/Anthropic: inference geography
    allow_speed: z.boolean().optional(), // Anthropic: speed mode control
    claude_beta_query: z.boolean().optional(), // Anthropic: beta query passthrough
    // Upstream model update settings (stored in settings JSON)
    upstream_model_update_check_enabled: z.boolean().optional(),
    upstream_model_update_auto_sync_enabled: z.boolean().optional(),
    upstream_model_update_ignored_models: z.string().optional(),
    // Channel availability schedule (stored in settings JSON)
    availability_schedule_enabled: z.boolean().optional(),
    availability_schedule_timezone: z.string().optional(),
    availability_schedule_windows: z
      .array(
        z.object({
          weekdays: z.array(z.number().int().min(1).max(7)).max(7),
          start: z.string(),
          end: z.string(),
        })
      )
      .max(32)
      .optional(),
    // Channel traffic control (stored in settings JSON)
    traffic_control_enabled: z.boolean().optional(),
    traffic_max_concurrency: z
      .number()
      .int()
      .min(0)
      .max(MAX_CHANNEL_TRAFFIC_CONCURRENCY)
      .optional(),
    traffic_rpm: z
      .number()
      .int()
      .min(0)
      .max(MAX_CHANNEL_TRAFFIC_RPM)
      .optional(),
    traffic_queue_size: z
      .number()
      .int()
      .min(0)
      .max(MAX_CHANNEL_TRAFFIC_QUEUE_SIZE)
      .optional(),
    traffic_queue_timeout_seconds: z
      .number()
      .int()
      .min(0)
      .max(MAX_CHANNEL_TRAFFIC_QUEUE_TIMEOUT_SECONDS)
      .optional(),
  })
  .superRefine((data, ctx) => {
    if ([3, 8, 36, 45].includes(data.type) && !data.base_url?.trim()) {
      addRequiredIssue(
        ctx,
        'base_url',
        'Base URL is required for this channel type'
      )
    }

    if (data.type === CHANNEL_TYPE_ADVANCED_CUSTOM) {
      const advancedCustomConfig = parseAdvancedCustomConfig(
        data.advanced_custom
      )
      const advancedCustomError =
        validateAdvancedCustomConfig(advancedCustomConfig)
      if (advancedCustomError) {
        addRequiredIssue(ctx, 'advanced_custom', advancedCustomError.message)
      }
      if (
        advancedCustomConfigUsesRelativeUpstreamPath(advancedCustomConfig) &&
        !data.base_url?.trim()
      ) {
        addRequiredIssue(
          ctx,
          'base_url',
          'Base URL is required when an advanced route uses an upstream path'
        )
      }
    }

    if ([3, 18, 21, 39, 41, 49].includes(data.type) && !data.other?.trim()) {
      addRequiredIssue(
        ctx,
        'other',
        'This channel type requires additional configuration'
      )
    }

    if (data.type === 57) {
      if (data.multi_key_mode && data.multi_key_mode !== 'single') {
        addRequiredIssue(
          ctx,
          'multi_key_mode',
          'Codex channels do not support batch creation'
        )
      }
      if (data.key?.trim() && !isCodexCredential(data.key)) {
        addRequiredIssue(
          ctx,
          'key',
          'Codex credential must be a JSON object with access_token and account_id'
        )
      }
    }

    if (
      data.type === 41 &&
      data.vertex_key_type === 'json' &&
      data.key?.trim() &&
      !isVertexJsonKey(data.key)
    ) {
      addRequiredIssue(
        ctx,
        'key',
        'Vertex AI service account key must be valid JSON'
      )
    }

    if (
      data.type === 41 &&
      data.vertex_key_type === 'api_key' &&
      data.multi_key_mode &&
      data.multi_key_mode !== 'single'
    ) {
      addRequiredIssue(
        ctx,
        'multi_key_mode',
        'Vertex AI API Key mode does not support batch creation'
      )
    }

    if (data.availability_schedule_enabled) {
      if (!data.availability_schedule_timezone?.trim()) {
        addRequiredIssue(
          ctx,
          'availability_schedule_timezone',
          'Timezone is required when scheduled availability is enabled'
        )
      }
      const windows = data.availability_schedule_windows || []
      if (windows.length === 0) {
        addRequiredIssue(
          ctx,
          'availability_schedule_windows',
          'Add at least one availability window'
        )
      }
      windows.forEach((window, index) => {
        if (window.weekdays.length === 0) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: ['availability_schedule_windows', index, 'weekdays'],
            message: 'Select at least one weekday',
          })
        }
        if (!AVAILABILITY_TIME_PATTERN.test(window.start)) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: ['availability_schedule_windows', index, 'start'],
            message: 'Use HH:mm in 24-hour format',
          })
        }
        if (!AVAILABILITY_TIME_PATTERN.test(window.end)) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: ['availability_schedule_windows', index, 'end'],
            message: 'Use HH:mm in 24-hour format',
          })
        }
        if (window.start === window.end) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: ['availability_schedule_windows', index, 'end'],
            message: 'Start and end times must differ',
          })
        }
      })
    }

    if (data.traffic_control_enabled) {
      if (!data.traffic_max_concurrency && !data.traffic_rpm) {
        addRequiredIssue(
          ctx,
          'traffic_max_concurrency',
          'Set maximum concurrency or requests per minute when traffic control is enabled.'
        )
      }
      if (
        (data.traffic_queue_size || 0) > 0 &&
        (data.traffic_queue_timeout_seconds || 0) <= 0
      ) {
        addRequiredIssue(
          ctx,
          'traffic_queue_timeout_seconds',
          'Queue timeout must be greater than 0 when queue capacity is enabled.'
        )
      }
    }
  })

export type ChannelFormValues = z.infer<typeof channelFormSchema>

// ============================================================================
// Default Form Values
// ============================================================================

export const CHANNEL_FORM_DEFAULT_VALUES: ChannelFormValues = {
  name: '',
  type: 1,
  base_url: '',
  key: '',
  openai_organization: '',
  models: '',
  group: ['default'],
  model_mapping: '',
  priority: 0,
  weight: 0,
  test_model: '',
  auto_ban: 1,
  status: CHANNEL_STATUS.ENABLED,
  status_code_mapping: '',
  tag: '',
  remark: '',
  setting: '',
  param_override: '',
  header_override: '',
  settings: '{}',
  other: '',
  multi_key_mode: 'single',
  multi_key_type: 'random',
  batch_add_set_key_prefix_2_name: false,
  key_mode: 'append',
  // Channel extra settings
  force_format: false,
  thinking_to_content: false,
  proxy: '',
  pass_through_body_enabled: false,
  system_prompt: '',
  system_prompt_override: false,
  // Type-specific settings
  is_enterprise_account: false,
  vertex_key_type: 'json',
  aws_key_type: 'ak_sk',
  azure_responses_version: '',
  // Field passthrough controls
  allow_service_tier: false,
  disable_store: false,
  allow_safety_identifier: false,
  allow_include_obfuscation: false,
  allow_inference_geo: false,
  allow_speed: false,
  claude_beta_query: false,
  upstream_model_update_check_enabled: false,
  upstream_model_update_auto_sync_enabled: false,
  upstream_model_update_ignored_models: '',
  availability_schedule_enabled: false,
  availability_schedule_timezone: 'Asia/Shanghai',
  availability_schedule_windows: DEFAULT_AVAILABILITY_WINDOWS,
  traffic_control_enabled: false,
  traffic_max_concurrency: 0,
  traffic_rpm: 0,
  traffic_queue_size: DEFAULT_CHANNEL_TRAFFIC_QUEUE_SIZE,
  traffic_queue_timeout_seconds: DEFAULT_CHANNEL_TRAFFIC_QUEUE_TIMEOUT_SECONDS,
  advanced_custom: '',
}

// ============================================================================
// Transform Functions
// ============================================================================

/**
 * Transform Channel from API to Form default values
 */
export function transformChannelToFormDefaults(
  channel: Channel
): ChannelFormValues {
  // Parse channel extra settings from setting field
  let extraSettings = {
    force_format: false,
    thinking_to_content: false,
    proxy: '',
    pass_through_body_enabled: false,
    system_prompt: '',
    system_prompt_override: false,
  }

  if (channel.setting) {
    try {
      const parsed = JSON.parse(channel.setting)
      extraSettings = {
        force_format: parsed.force_format || false,
        thinking_to_content: parsed.thinking_to_content || false,
        proxy: parsed.proxy || '',
        pass_through_body_enabled: parsed.pass_through_body_enabled || false,
        system_prompt: parsed.system_prompt || '',
        system_prompt_override: parsed.system_prompt_override || false,
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel setting:', error)
    }
  }

  // Parse type-specific settings from settings field
  let vertexKeyType: 'json' | 'api_key' = 'json'
  let azureResponsesVersion = ''
  let isEnterpriseAccount = false
  let awsKeyType: 'ak_sk' | 'api_key' = 'ak_sk'
  let allowServiceTier = false
  let disableStore = false
  let allowSafetyIdentifier = false
  let allowIncludeObfuscation = false
  let allowInferenceGeo = false
  let allowSpeed = false
  let claudeBetaQuery = false
  let upstreamModelUpdateCheckEnabled = false
  let upstreamModelUpdateAutoSyncEnabled = false
  let upstreamModelUpdateIgnoredModels = ''
  let availabilityScheduleEnabled = false
  let availabilityScheduleTimezone = 'Asia/Shanghai'
  let availabilityScheduleWindows = DEFAULT_AVAILABILITY_WINDOWS.map(
    (window) => ({ ...window, weekdays: [...window.weekdays] })
  )
  let trafficControlEnabled = false
  let trafficMaxConcurrency = 0
  let trafficRpm = 0
  let trafficQueueSize = DEFAULT_CHANNEL_TRAFFIC_QUEUE_SIZE
  let trafficQueueTimeoutSeconds = DEFAULT_CHANNEL_TRAFFIC_QUEUE_TIMEOUT_SECONDS
  let advancedCustom = ''

  if (channel.settings) {
    try {
      const parsed = JSON.parse(channel.settings)
      vertexKeyType = parsed.vertex_key_type || 'json'
      azureResponsesVersion = parsed.azure_responses_version || ''
      isEnterpriseAccount = parsed.openrouter_enterprise === true
      awsKeyType = parsed.aws_key_type || 'ak_sk'
      allowServiceTier = parsed.allow_service_tier === true
      disableStore = parsed.disable_store === true
      allowSafetyIdentifier = parsed.allow_safety_identifier === true
      allowIncludeObfuscation = parsed.allow_include_obfuscation === true
      allowInferenceGeo = parsed.allow_inference_geo === true
      allowSpeed = parsed.allow_speed === true
      claudeBetaQuery = parsed.claude_beta_query === true
      upstreamModelUpdateCheckEnabled =
        parsed.upstream_model_update_check_enabled === true
      upstreamModelUpdateAutoSyncEnabled =
        parsed.upstream_model_update_auto_sync_enabled === true
      upstreamModelUpdateIgnoredModels = Array.isArray(
        parsed.upstream_model_update_ignored_models
      )
        ? parsed.upstream_model_update_ignored_models.join(',')
        : ''
      if (
        parsed.availability_schedule &&
        typeof parsed.availability_schedule === 'object'
      ) {
        availabilityScheduleEnabled =
          parsed.availability_schedule.enabled === true
        availabilityScheduleTimezone =
          typeof parsed.availability_schedule.timezone === 'string' &&
          parsed.availability_schedule.timezone.trim()
            ? parsed.availability_schedule.timezone.trim()
            : 'Asia/Shanghai'
        if (Array.isArray(parsed.availability_schedule.windows)) {
          const normalizedWindows = parsed.availability_schedule.windows
            .map((window: unknown) => {
              if (!isJsonObjectValue(window)) return null
              const weekdays = Array.isArray(window.weekdays)
                ? [
                    ...new Set(
                      window.weekdays
                        .map((weekday) => Number(weekday))
                        .filter(
                          (weekday) =>
                            Number.isInteger(weekday) &&
                            weekday >= 1 &&
                            weekday <= 7
                        )
                    ),
                  ].sort((a, b) => a - b)
                : []
              const start = String(window.start || '')
              const end = String(window.end || '')
              if (!weekdays.length || !start || !end) return null
              return { weekdays, start, end }
            })
            .filter(
              (
                window: {
                  weekdays: number[]
                  start: string
                  end: string
                } | null
              ): window is { weekdays: number[]; start: string; end: string } =>
                window !== null
            )
          if (normalizedWindows.length > 0) {
            availabilityScheduleWindows = normalizedWindows
          }
        }
      }
      if (isJsonObjectValue(parsed.traffic_control)) {
        trafficControlEnabled = parsed.traffic_control.enabled === true
        trafficMaxConcurrency = normalizeNonNegativeInteger(
          parsed.traffic_control.max_concurrency,
          0,
          MAX_CHANNEL_TRAFFIC_CONCURRENCY
        )
        trafficRpm = normalizeNonNegativeInteger(
          parsed.traffic_control.rpm,
          0,
          MAX_CHANNEL_TRAFFIC_RPM
        )
        trafficQueueSize = normalizeNonNegativeInteger(
          parsed.traffic_control.queue_size,
          DEFAULT_CHANNEL_TRAFFIC_QUEUE_SIZE,
          MAX_CHANNEL_TRAFFIC_QUEUE_SIZE
        )
        trafficQueueTimeoutSeconds = normalizeNonNegativeInteger(
          parsed.traffic_control.queue_timeout_seconds,
          DEFAULT_CHANNEL_TRAFFIC_QUEUE_TIMEOUT_SECONDS,
          MAX_CHANNEL_TRAFFIC_QUEUE_TIMEOUT_SECONDS
        )
      }
      if (parsed.advanced_custom) {
        advancedCustom = stringifyAdvancedCustomConfig(parsed.advanced_custom)
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel settings:', error)
    }
  }

  return {
    name: channel.name || '',
    type: channel.type,
    base_url: channel.base_url || '',
    key: '', // Never populate key from backend for security
    openai_organization: channel.openai_organization || '',
    models: channel.models || '',
    group: parseGroups(channel.group || 'default'),
    model_mapping: channel.model_mapping || '',
    priority: channel.priority || 0,
    weight: channel.weight || 0,
    test_model: channel.test_model || '',
    auto_ban: channel.auto_ban ?? 1,
    status: channel.status,
    status_code_mapping: channel.status_code_mapping || '',
    tag: channel.tag || '',
    remark: channel.remark || '',
    setting: channel.setting || '',
    param_override: channel.param_override || '',
    header_override: channel.header_override || '',
    settings: channel.settings || '{}',
    other: channel.other || '',
    multi_key_mode: 'single',
    multi_key_type: channel.channel_info.multi_key_mode || 'random',
    batch_add_set_key_prefix_2_name: false,
    key_mode: 'append', // Default to append mode for editing multi-key channels
    // Channel extra settings
    ...extraSettings,
    // Type-specific settings
    is_enterprise_account: isEnterpriseAccount,
    vertex_key_type: vertexKeyType,
    azure_responses_version: azureResponsesVersion,
    aws_key_type: awsKeyType,
    allow_service_tier: allowServiceTier,
    disable_store: disableStore,
    allow_include_obfuscation: allowIncludeObfuscation,
    allow_inference_geo: allowInferenceGeo,
    allow_speed: allowSpeed,
    claude_beta_query: claudeBetaQuery,
    allow_safety_identifier: allowSafetyIdentifier,
    upstream_model_update_check_enabled: upstreamModelUpdateCheckEnabled,
    upstream_model_update_auto_sync_enabled: upstreamModelUpdateAutoSyncEnabled,
    upstream_model_update_ignored_models: upstreamModelUpdateIgnoredModels,
    availability_schedule_enabled: availabilityScheduleEnabled,
    availability_schedule_timezone: availabilityScheduleTimezone,
    availability_schedule_windows: availabilityScheduleWindows,
    traffic_control_enabled: trafficControlEnabled,
    traffic_max_concurrency: trafficMaxConcurrency,
    traffic_rpm: trafficRpm,
    traffic_queue_size: trafficQueueSize,
    traffic_queue_timeout_seconds: trafficQueueTimeoutSeconds,
    advanced_custom: advancedCustom,
  }
}

/**
 * Build the setting JSON string from form extra settings
 */
function buildSettingJSON(formData: ChannelFormValues): string {
  const settingObj = {
    force_format: formData.force_format || false,
    thinking_to_content: formData.thinking_to_content || false,
    proxy: formData.proxy || '',
    pass_through_body_enabled: formData.pass_through_body_enabled || false,
    system_prompt: formData.system_prompt || '',
    system_prompt_override: formData.system_prompt_override || false,
  }
  return JSON.stringify(settingObj)
}

/**
 * Build the settings JSON string (for type-specific config like vertex_key_type)
 */
function buildSettingsJSON(formData: ChannelFormValues): string {
  let settingsObj: Record<string, unknown> = {}

  // Try to parse existing settings first
  if (formData.settings && formData.settings !== '{}') {
    try {
      settingsObj = JSON.parse(formData.settings)
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse existing settings:', error)
    }
  }

  // Add vertex_key_type for Vertex AI channels (type 41)
  if (formData.type === 41) {
    settingsObj.vertex_key_type = formData.vertex_key_type || 'json'
  } else if ('vertex_key_type' in settingsObj) {
    delete settingsObj.vertex_key_type
  }

  // Add azure_responses_version for Azure channels (type 3)
  if (formData.type === 3 && formData.azure_responses_version) {
    settingsObj.azure_responses_version = formData.azure_responses_version
  } else if ('azure_responses_version' in settingsObj) {
    delete settingsObj.azure_responses_version
  }

  // Add enterprise account setting for OpenRouter (type 20)
  if (formData.type === 20) {
    settingsObj.openrouter_enterprise = formData.is_enterprise_account === true
  } else if ('openrouter_enterprise' in settingsObj) {
    delete settingsObj.openrouter_enterprise
  }

  // Add aws_key_type for AWS channels (type 33)
  if (formData.type === 33) {
    settingsObj.aws_key_type = formData.aws_key_type || 'ak_sk'
  } else if ('aws_key_type' in settingsObj) {
    delete settingsObj.aws_key_type
  }

  // Field passthrough controls:
  // - OpenAI (type 1) and Anthropic (type 14): allow_service_tier
  // - OpenAI only: disable_store, allow_safety_identifier
  if (formData.type === 1 || formData.type === 14) {
    settingsObj.allow_service_tier = formData.allow_service_tier === true
  } else if ('allow_service_tier' in settingsObj) {
    delete settingsObj.allow_service_tier
  }

  if (formData.type === 1) {
    settingsObj.disable_store = formData.disable_store === true
    settingsObj.allow_safety_identifier =
      formData.allow_safety_identifier === true
    settingsObj.allow_include_obfuscation =
      formData.allow_include_obfuscation === true
    settingsObj.allow_inference_geo = formData.allow_inference_geo === true
  } else {
    if ('disable_store' in settingsObj) delete settingsObj.disable_store
    if ('allow_safety_identifier' in settingsObj)
      delete settingsObj.allow_safety_identifier
    if ('allow_include_obfuscation' in settingsObj)
      delete settingsObj.allow_include_obfuscation
    if (formData.type !== 14 && 'allow_inference_geo' in settingsObj)
      delete settingsObj.allow_inference_geo
  }

  // Anthropic (type 14): claude_beta_query, allow_inference_geo, allow_speed
  if (formData.type === 14) {
    settingsObj.allow_inference_geo = formData.allow_inference_geo === true
    settingsObj.allow_speed = formData.allow_speed === true
    settingsObj.claude_beta_query = formData.claude_beta_query === true
  } else {
    if ('allow_speed' in settingsObj) delete settingsObj.allow_speed
    if ('claude_beta_query' in settingsObj) delete settingsObj.claude_beta_query
  }

  // Upstream model update settings (for model-fetchable channel types)
  if (MODEL_FETCHABLE_TYPES.has(formData.type)) {
    settingsObj.upstream_model_update_check_enabled =
      formData.upstream_model_update_check_enabled === true
    settingsObj.upstream_model_update_auto_sync_enabled =
      settingsObj.upstream_model_update_check_enabled === true &&
      formData.upstream_model_update_auto_sync_enabled === true
    settingsObj.upstream_model_update_ignored_models = Array.from(
      new Set(
        String(formData.upstream_model_update_ignored_models || '')
          .split(',')
          .map((model) => model.trim())
          .filter(Boolean)
      )
    )
    if (
      !Array.isArray(settingsObj.upstream_model_update_last_detected_models) ||
      settingsObj.upstream_model_update_check_enabled !== true
    ) {
      settingsObj.upstream_model_update_last_detected_models = []
    }
    if (typeof settingsObj.upstream_model_update_last_check_time !== 'number') {
      settingsObj.upstream_model_update_last_check_time = 0
    }
  }

  const hadAvailabilitySchedule = isJsonObjectValue(
    settingsObj.availability_schedule
  )
  if (formData.availability_schedule_enabled || hadAvailabilitySchedule) {
    settingsObj.availability_schedule = {
      enabled: formData.availability_schedule_enabled === true,
      timezone:
        formData.availability_schedule_timezone?.trim() || 'Asia/Shanghai',
      windows: (formData.availability_schedule_windows || []).map((window) => ({
        weekdays: [...new Set(window.weekdays)].sort((a, b) => a - b),
        start: window.start,
        end: window.end,
      })),
    }
  } else if ('availability_schedule' in settingsObj) {
    delete settingsObj.availability_schedule
  }

  const hadTrafficControl = isJsonObjectValue(settingsObj.traffic_control)
  if (formData.traffic_control_enabled || hadTrafficControl) {
    settingsObj.traffic_control = {
      enabled: formData.traffic_control_enabled === true,
      max_concurrency: Number(formData.traffic_max_concurrency || 0),
      rpm: Number(formData.traffic_rpm || 0),
      queue_size: Number(formData.traffic_queue_size || 0),
      queue_timeout_seconds: Number(
        formData.traffic_queue_timeout_seconds || 0
      ),
    }
  } else if ('traffic_control' in settingsObj) {
    delete settingsObj.traffic_control
  }

  if (formData.type === CHANNEL_TYPE_ADVANCED_CUSTOM) {
    const advancedCustomConfig = parseAdvancedCustomConfig(
      formData.advanced_custom
    )
    if (advancedCustomConfig) {
      settingsObj.advanced_custom = advancedCustomConfig
    }
  } else if ('advanced_custom' in settingsObj) {
    delete settingsObj.advanced_custom
  }

  return JSON.stringify(settingsObj)
}

function normalizeBaseUrl(value: string | undefined): string {
  return String(value || '')
    .trim()
    .replace(/\/+$/, '')
}

/**
 * Transform form data to API payload for creating channel
 */
export function transformFormDataToCreatePayload(formData: ChannelFormValues): {
  mode: 'single' | 'batch' | 'multi_to_single'
  multi_key_mode?: 'random' | 'polling'
  batch_add_set_key_prefix_2_name?: boolean
  channel: Partial<Channel>
} {
  const mode = formData.multi_key_mode || 'single'

  const channel: Partial<Channel> = {
    name: formData.name,
    type: formData.type,
    base_url: normalizeBaseUrl(formData.base_url) || null,
    key: formData.key,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority || null,
    weight: formData.weight || null,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status: formData.status,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  // Clean up empty strings to null for optional fields
  Object.keys(channel).forEach((key) => {
    if (channel[key as keyof typeof channel] === '') {
      ;(channel as Record<string, unknown>)[key] = null
    }
  })

  return {
    mode,
    multi_key_mode:
      mode === 'multi_to_single' ? formData.multi_key_type : undefined,
    batch_add_set_key_prefix_2_name:
      mode === 'batch' ? formData.batch_add_set_key_prefix_2_name : undefined,
    channel,
  }
}

/**
 * Transform form data to API payload for updating channel
 */
export function transformFormDataToUpdatePayload(
  formData: ChannelFormValues,
  channelId: number
): Partial<Channel> {
  const payload: Partial<Channel> = {
    id: channelId,
    name: formData.name,
    type: formData.type,
    base_url: normalizeBaseUrl(formData.base_url) || null,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority ?? 0,
    weight: formData.weight ?? 0,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status: formData.status,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  // Only include key if it was changed (not empty)
  if (formData.key && formData.key.trim()) {
    payload.key = formData.key
  }

  // Clean up empty strings to null for optional fields
  Object.keys(payload).forEach((key) => {
    if (payload[key as keyof typeof payload] === '') {
      ;(payload as Record<string, unknown>)[key] = null
    }
  })

  // Send explicit empty strings for nullable fields so GORM updates can clear them.
  payload.base_url = normalizeBaseUrl(formData.base_url) || ''
  payload.openai_organization = formData.openai_organization || ''
  payload.test_model = formData.test_model || ''
  payload.tag = formData.tag || ''
  payload.remark = formData.remark || ''
  payload.model_mapping = formData.model_mapping || ''
  payload.status_code_mapping = formData.status_code_mapping || ''
  payload.param_override = formData.param_override || ''
  payload.header_override = formData.header_override || ''

  return payload
}

// ============================================================================
// Validation Helpers
// ============================================================================

/**
 * Validate JSON string
 */
export function validateJSON(value: string): boolean {
  if (!value || value.trim() === '') return true
  try {
    JSON.parse(value)
    return true
  } catch {
    return false
  }
}

/**
 * Validate model mapping format
 */
export function validateModelMapping(value: string): boolean {
  if (!value || value.trim() === '') return true
  return validateJSON(value)
}

/**
 * Parse models string to array
 */
export function parseModels(models: string): string[] {
  if (!models) return []
  return models
    .split(',')
    .map((m) => m.trim())
    .filter((m) => m.length > 0)
}

/**
 * Parse groups string to array
 */
export function parseGroups(groups: string): string[] {
  if (!groups) return []
  return groups
    .split(',')
    .map((g) => g.trim())
    .filter((g) => g.length > 0)
}

/**
 * Format models array to string
 */
export function formatModels(models: string[]): string {
  return models.join(',')
}

/**
 * Format groups array to string
 */
export function formatGroups(groups: string[]): string {
  return groups.join(',')
}
