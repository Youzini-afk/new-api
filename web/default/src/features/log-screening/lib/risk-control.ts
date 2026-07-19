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
import type { RiskSignalSummary } from '../types'

export function parseRiskSignals(raw: string): RiskSignalSummary | null {
  if (!raw) return null
  try {
    return JSON.parse(raw) as RiskSignalSummary
  } catch {
    return null
  }
}

export function verdictLabel(verdict: string): string {
  const labels: Record<string, string> = {
    normal: 'Normal',
    small_share: 'Small personal sharing',
    key_leak: 'Key leak',
    gateway_distribution: 'Gateway distribution',
    multi_node_gateway: 'Multi-node gateway',
    commercial_resale: 'Commercial resale',
    forbidden_paid_client: 'Forbidden paid client',
    uncertain: 'Uncertain',
  }
  return labels[verdict] ?? verdict
}

export function actionLabel(action: string): string {
  const labels: Record<string, string> = {
    none: 'No action',
    observe: 'Observe',
    rate_limit: 'Rate limit',
    freeze_token: 'Freeze token',
    temporary_block: 'Temporary block',
    permanent_ban: 'Permanent ban',
    manual_review: 'Manual review',
    clear: 'Clear restriction',
  }
  return labels[action] ?? action
}

export function statusLabel(status: string): string {
  const labels: Record<string, string> = {
    open: 'Open',
    reviewing: 'Reviewing',
    actioned: 'Actioned',
    resolved: 'Resolved',
    dismissed: 'Dismissed',
  }
  return labels[status] ?? status
}
