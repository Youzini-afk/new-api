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
 * API functions for log screening (Phase 5 admin-only endpoints).
 */
import { api } from '@/lib/api'

import type {
  LogScreeningCleanupResult,
  LogScreeningListParams,
  LogScreeningPage,
  LogScreeningResponse,
  LogScreeningRecordItem,
  LogScreeningRunSummary,
  PromptBlockLogDetail,
  PromptBlockLogItem,
  PromptBlockLogListParams,
  UABlockLogDetail,
  UABlockLogItem,
  UABlockLogListParams,
  RiskActionItem,
  RiskActionRequest,
  RiskCaseDetail,
  RiskCaseItem,
  RiskCaseListParams,
  RiskControlSetting,
  RiskRunTaskResult,
} from './types'

// ============================================================================
// Screening records
// ============================================================================

export function getLogScreeningRecords(
  params: LogScreeningListParams
): Promise<LogScreeningResponse<LogScreeningPage<LogScreeningRecordItem>>> {
  return api
    .get('/api/log_screening/records', { params })
    .then((res) => res.data)
}

export function runLogScreening(
  kind?: string
): Promise<LogScreeningResponse<LogScreeningRunSummary>> {
  return api
    .post('/api/log_screening/run', kind ? { kind } : {})
    .then((res) => res.data)
}

export function cleanupLogScreeningRecords(): Promise<
  LogScreeningResponse<LogScreeningCleanupResult>
> {
  return api.post('/api/log_screening/cleanup').then((res) => res.data)
}

export function appendScreeningRemark(
  recordId: number,
  remark: string
): Promise<LogScreeningResponse<{ id: number }>> {
  return api
    .post(`/api/log_screening/records/${recordId}/remark`, { remark })
    .then((res) => res.data)
}

// ============================================================================
// Prompt block logs
// ============================================================================

export function getPromptBlockLogs(
  params: PromptBlockLogListParams
): Promise<LogScreeningResponse<LogScreeningPage<PromptBlockLogItem>>> {
  return api
    .get('/api/log_screening/prompt_block_logs', { params })
    .then((res) => res.data)
}

export function getPromptBlockLogDetail(
  logId: number
): Promise<LogScreeningResponse<PromptBlockLogDetail>> {
  return api
    .get(`/api/log_screening/prompt_block_logs/${logId}`)
    .then((res) => res.data)
}

export function appendPromptBlockRemark(
  logId: number,
  remark: string
): Promise<LogScreeningResponse<{ id: number }>> {
  return api
    .post(`/api/log_screening/prompt_block_logs/${logId}/remark`, { remark })
    .then((res) => res.data)
}

// ============================================================================
// UA block logs
// ============================================================================

export function getUABlockLogs(
  params: UABlockLogListParams
): Promise<LogScreeningResponse<LogScreeningPage<UABlockLogItem>>> {
  return api
    .get('/api/log_screening/ua_block_logs', { params })
    .then((res) => res.data)
}

export function getUABlockLogDetail(
  logId: number
): Promise<LogScreeningResponse<UABlockLogDetail>> {
  return api
    .get(`/api/log_screening/ua_block_logs/${logId}`)
    .then((res) => res.data)
}

export function appendUABlockRemark(
  logId: number,
  remark: string
): Promise<LogScreeningResponse<{ id: number }>> {
  return api
    .post(`/api/log_screening/ua_block_logs/${logId}/remark`, { remark })
    .then((res) => res.data)
}

// ============================================================================
// Comprehensive risk control
// ============================================================================

export function getRiskCases(
  params: RiskCaseListParams
): Promise<LogScreeningResponse<LogScreeningPage<RiskCaseItem>>> {
  return api.get('/api/risk_control/cases', { params }).then((res) => res.data)
}

export function getRiskCaseDetail(
  caseId: number
): Promise<LogScreeningResponse<RiskCaseDetail>> {
  return api.get(`/api/risk_control/cases/${caseId}`).then((res) => res.data)
}

export function analyzeRiskCase(
  caseId: number
): Promise<LogScreeningResponse<RiskCaseItem>> {
  return api
    .post(`/api/risk_control/cases/${caseId}/analyze`)
    .then((res) => res.data)
}

export function reviewRiskCase(
  caseId: number,
  status: string,
  note: string
): Promise<LogScreeningResponse<{ id: number }>> {
  return api
    .post(`/api/risk_control/cases/${caseId}/review`, { status, note })
    .then((res) => res.data)
}

export function applyRiskCaseAction(
  caseId: number,
  request: RiskActionRequest
): Promise<LogScreeningResponse<RiskActionItem>> {
  return api
    .post(`/api/risk_control/cases/${caseId}/action`, request)
    .then((res) => res.data)
}

export function runRiskControl(): Promise<
  LogScreeningResponse<RiskRunTaskResult>
> {
  return api.post('/api/risk_control/run').then((res) => res.data)
}

export function getRiskControlSetting(): Promise<
  LogScreeningResponse<RiskControlSetting>
> {
  return api.get('/api/risk_control/settings').then((res) => res.data)
}

export function saveRiskControlSetting(
  setting: RiskControlSetting
): Promise<LogScreeningResponse<RiskControlSetting>> {
  return api.put('/api/risk_control/settings', setting).then((res) => res.data)
}
