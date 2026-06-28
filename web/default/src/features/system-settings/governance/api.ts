import { api } from '@/lib/api'
import type {
  ErrorGovernanceAIOrganizeResult,
  ErrorGovernanceAISetting,
  RelayErrorGovernanceConfig,
} from './types'

export type GovernanceAPIResponse<T> = {
  success: boolean
  message?: string
  data?: T
}

export function getErrorGovernanceAISetting(): Promise<
  GovernanceAPIResponse<ErrorGovernanceAISetting>
> {
  return api.get('/api/error_insight/governance-ai/settings').then((res) => res.data)
}

export function saveErrorGovernanceAISetting(
  data: ErrorGovernanceAISetting
): Promise<GovernanceAPIResponse<ErrorGovernanceAISetting>> {
  return api.put('/api/error_insight/governance-ai/settings', data).then((res) => res.data)
}

export function generateErrorGovernanceAIOrganization(
  governanceConfig: RelayErrorGovernanceConfig
): Promise<
  GovernanceAPIResponse<ErrorGovernanceAIOrganizeResult>
> {
  return api
    .post('/api/error_insight/governance-ai/organize', {
      governance_config: governanceConfig,
    })
    .then((res) => res.data)
}
