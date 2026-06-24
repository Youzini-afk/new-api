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
 * Main log screening page shell with tabs.
 */
import { useCallback } from 'react'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { ShieldCheck } from 'lucide-react'
import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'
import {
  LOG_SCREENING_DEFAULT_TAB,
  LOG_SCREENING_TABS,
  type LogScreeningTabId,
} from '../constants'
import { ScreeningRecordsTab } from './screening-records-tab'
import { PromptBlocksTab } from './prompt-blocks-tab'
import { UABlocksTab } from './ua-blocks-tab'
import { ScreeningSettingsTab } from './screening-settings-tab'

const route = getRouteApi('/_authenticated/log-screening/$tab')

const TAB_COMPONENTS: Record<LogScreeningTabId, React.ComponentType> = {
  records: ScreeningRecordsTab,
  'prompt-blocks': PromptBlocksTab,
  'ua-blocks': UABlocksTab,
  settings: ScreeningSettingsTab,
}

export function LogScreeningPage() {
  const { t } = useTranslation()
  const params = route.useParams()
  const navigate = useNavigate()
  const role = useAuthStore((state) => state.auth.user?.role ?? ROLE.GUEST)
  const visibleTabs = LOG_SCREENING_TABS.filter(
    (tab) => tab.id !== 'settings' || role >= ROLE.SUPER_ADMIN
  )

  const activeTab = isLogScreeningTabId(params.tab)
    ? params.tab
    : LOG_SCREENING_DEFAULT_TAB

  const handleTabChange = useCallback(
    (tab: string) => {
      if (!isLogScreeningTabId(tab)) return
      void navigate({
        to: '/log-screening/$tab',
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
          <ShieldCheck className='size-5' />
          {t('Log Screening')}
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-4'>
          <Tabs value={activeTab} onValueChange={handleTabChange}>
            <TabsList className='w-full justify-start overflow-x-auto sm:w-auto'>
              {visibleTabs.map((tab) => (
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

function isLogScreeningTabId(value: string): value is LogScreeningTabId {
  return LOG_SCREENING_TABS.some((tab) => tab.id === value)
}
