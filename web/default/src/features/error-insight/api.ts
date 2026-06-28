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
 * API functions for the admin error insight feature.
 */
import { api } from '@/lib/api'
import type {
  ErrorInsightAIGenerateResult,
  ErrorInsightAISetting,
  ErrorInsightDeleteResult,
  ErrorInsightFilterParams,
  ErrorInsightLogsData,
  ErrorInsightResponse,
  ErrorInsightAIRuleSuggestion,
  ErrorInsightSignature,
  ErrorInsightSummary,
} from './types'

// ============================================================================
// Summary
// ============================================================================

export function getErrorInsightSummary(
  params?: ErrorInsightFilterParams
): Promise<ErrorInsightResponse<ErrorInsightSummary>> {
  return api
    .get('/api/error_insight/summary', { params })
    .then((res) => res.data)
}

// ============================================================================
// Signatures
// ============================================================================

export function getErrorInsightSignatures(
  params?: ErrorInsightFilterParams
): Promise<ErrorInsightResponse<ErrorInsightSignature[]>> {
  return api
    .get('/api/error_insight/signatures', { params })
    .then((res) => res.data)
}

// ============================================================================
// Logs (paginated)
// ============================================================================

export interface GetErrorInsightLogsParams extends ErrorInsightFilterParams {
  page?: number
  page_size?: number
}

export function getErrorInsightLogs(
  params: GetErrorInsightLogsParams
): Promise<ErrorInsightResponse<ErrorInsightLogsData>> {
  return api
    .get('/api/error_insight/logs', { params })
    .then((res) => res.data)
}

// ============================================================================
// Delete a signature
// ============================================================================

export function deleteErrorInsightSignature(
  signature: string
): Promise<ErrorInsightResponse<ErrorInsightDeleteResult>> {
  return api
    .delete(
      `/api/error_insight/signatures/${encodeURIComponent(signature)}`
    )
    .then((res) => res.data)
}

export function getErrorInsightAISetting(): Promise<
  ErrorInsightResponse<ErrorInsightAISetting>
> {
  return api.get('/api/error_insight/ai/settings').then((res) => res.data)
}

export function saveErrorInsightAISetting(
  data: ErrorInsightAISetting
): Promise<ErrorInsightResponse<ErrorInsightAISetting>> {
  return api.put('/api/error_insight/ai/settings', data).then((res) => res.data)
}

export function generateErrorInsightAIRules(
  signature: string
): Promise<ErrorInsightResponse<ErrorInsightAIGenerateResult>> {
  return api
    .post('/api/error_insight/ai/generate', { signature })
    .then((res) => res.data)
}

export function getErrorInsightAIResult(
  signature: string
): Promise<ErrorInsightResponse<ErrorInsightAIGenerateResult | null>> {
  return api
    .get(`/api/error_insight/ai/results/${encodeURIComponent(signature)}`)
    .then((res) => res.data)
}

export function saveErrorInsightAIRule(
  rule: ErrorInsightAIRuleSuggestion,
  signature?: string
): Promise<ErrorInsightResponse<ErrorInsightAIRuleSuggestion>> {
  return api
    .post('/api/error_insight/ai/rules', { rule, signature })
    .then((res) => res.data)
}
