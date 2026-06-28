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
 * Error insight admin page shell.
 *
 * Surfaces the error-insight summary cards plus a tabbed view that toggles
 * between the aggregated signatures table and the raw logs list. All filters
 * drive every request so the summary, signatures and logs stay consistent.
 */
import { useCallback, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ShieldAlert } from 'lucide-react'
import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  ERROR_INSIGHT_DEFAULT_TAB,
  ERROR_INSIGHT_TABS,
  type ErrorInsightTabId,
  type RuleMatchedFilter,
  type TimeRangePreset,
} from '../constants'
import type { ErrorInsightFilterParams } from '../types'
import { getErrorInsightSummary } from '../api'
import { SummaryCards } from './summary-cards'
import { FiltersBar, type FiltersValue } from './filters-bar'
import { SignaturesTable } from './signatures-table'
import { LogsTable } from './logs-table'
import { AISettingsDialog } from './ai-settings-dialog'

function isTabId(value: string): value is ErrorInsightTabId {
  return ERROR_INSIGHT_TABS.some((tab) => tab.id === value)
}

export function ErrorInsightPage() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<ErrorInsightTabId>(
    ERROR_INSIGHT_DEFAULT_TAB
  )
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [filters, setFilters] = useState<FiltersValue>({
    timeRange: 86400,
    ruleMatched: 'unmatched',
    ruleCode: '',
    unmatchedReason: '',
    modelName: '',
  })
  const buildParams = useCallback(
    (value: FiltersValue): ErrorInsightFilterParams => {
      const params: ErrorInsightFilterParams = {}
      if (value.timeRange > 0) {
        const now = Math.floor(Date.now() / 1000)
        params.start_timestamp = now - value.timeRange
        params.end_timestamp = now
      }
      if (value.ruleMatched === 'matched') params.rule_matched = true
      else if (value.ruleMatched === 'unmatched') params.rule_matched = false
      if (value.ruleCode.trim()) params.rule_code = value.ruleCode.trim()
      if (value.unmatchedReason.trim())
        params.unmatched_reason = value.unmatchedReason.trim()
      if (value.modelName.trim()) params.model_name = value.modelName.trim()
      return params
    },
    []
  )

  const apiParams = useMemo(() => buildParams(filters), [filters, buildParams])

  const {
    data: summary,
    isLoading: summaryLoading,
    error: summaryError,
    refetch: refetchSummary,
  } = useQuery({
    queryKey: ['error-insight', 'summary', apiParams],
    queryFn: async () => {
      const result = await getErrorInsightSummary(apiParams)
      if (!result.success) {
        throw new Error(result.message || t('Failed to load summary'))
      }
      return result.data
    },
  })

  const handleTabChange = useCallback((tab: string) => {
    if (!isTabId(tab)) return
    setActiveTab(tab)
  }, [])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex items-center gap-2'>
          <ShieldAlert className='size-5' />
          {t('Error Insight')}
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-7'>
          <SummaryCards
            data={summary}
            isLoading={summaryLoading}
            error={summaryError}
            onRetry={refetchSummary}
          />

          <FiltersBar value={filters} onChange={setFilters} />

          <Tabs
            value={activeTab}
            onValueChange={handleTabChange}
            className='flex flex-col gap-4'
          >
            <TabsList className='bg-card/70 ring-foreground/10 w-full justify-start rounded-xl ring-1 sm:w-auto'>
              {ERROR_INSIGHT_TABS.map((tab) => (
                <TabsTrigger key={tab.id} value={tab.id}>
                  {t(tab.titleKey)}
                </TabsTrigger>
              ))}
            </TabsList>

            <div>
              {activeTab === 'signatures' ? (
                <SignaturesTable
                  params={apiParams}
                  onOpenAISettings={() => setSettingsOpen(true)}
                />
              ) : (
                <LogsTable params={apiParams} />
              )}
            </div>
          </Tabs>
        </div>
        <AISettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

// Re-export the types used by child components so the page owns the contract.
export type { FiltersValue, RuleMatchedFilter, TimeRangePreset }
