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
 * Main operations stats page shell with tabs.
 */
import { useCallback } from 'react'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { BarChart3 } from 'lucide-react'
import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  OPERATIONS_STATS_DEFAULT_TAB,
  OPERATIONS_STATS_TABS,
  type OperationsStatsTabId,
} from '../constants'
import { IPStatsTab } from './ip-stats-tab'
import { UAStatsTab } from './ua-stats-tab'
import { LeaderboardTab } from './leaderboard-tab'
import { KeyLookupTab } from './key-lookup-tab'

const route = getRouteApi('/_authenticated/operations-stats/$tab')

const TAB_COMPONENTS: Record<OperationsStatsTabId, React.ComponentType> = {
  ip: IPStatsTab,
  ua: UAStatsTab,
  leaderboard: LeaderboardTab,
  'key-lookup': KeyLookupTab,
}

export function OperationsStatsPage() {
  const { t } = useTranslation()
  const params = route.useParams()
  const navigate = useNavigate()

  const activeTab = isOperationsStatsTabId(params.tab)
    ? params.tab
    : OPERATIONS_STATS_DEFAULT_TAB

  const handleTabChange = useCallback(
    (tab: string) => {
      if (!isOperationsStatsTabId(tab)) return
      void navigate({
        to: '/operations-stats/$tab',
        params: { tab },
      })
    },
    [navigate]
  )

  const TabComponent = TAB_COMPONENTS[activeTab]

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        <span className='inline-flex items-center gap-2'>
          <BarChart3 className='size-5' />
          {t('Operations Stats')}
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-4'>
          <Tabs value={activeTab} onValueChange={handleTabChange}>
            <TabsList className='w-full justify-start sm:w-auto'>
              {OPERATIONS_STATS_TABS.map((tab) => (
                <TabsTrigger key={tab.id} value={tab.id}>
                  {t(tab.titleKey)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>

          <div className='min-h-0 flex-1'>
            <TabComponent />
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function isOperationsStatsTabId(value: string): value is OperationsStatsTabId {
  return OPERATIONS_STATS_TABS.some((tab) => tab.id === value)
}
