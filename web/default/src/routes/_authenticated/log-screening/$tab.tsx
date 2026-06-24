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
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { LogScreeningPage } from '@/features/log-screening'
import {
  LOG_SCREENING_DEFAULT_TAB,
  LOG_SCREENING_TABS,
} from '@/features/log-screening/constants'

export const Route = createFileRoute('/_authenticated/log-screening/$tab')({
  beforeLoad: ({ params, location }) => {
    const { auth } = useAuthStore.getState()
    const role = auth.user?.role ?? ROLE.GUEST

    if (role < ROLE.ADMIN) {
      throw redirect({
        to: '/403',
      })
    }

    if (params.tab === 'settings' && role < ROLE.SUPER_ADMIN) {
      throw redirect({
        to: '/403',
      })
    }

    const validTabs = LOG_SCREENING_TABS.map((tab) => tab.id)
    if (!validTabs.includes(params.tab as (typeof validTabs)[number])) {
      throw redirect({
        to: '/log-screening/$tab',
        params: { tab: LOG_SCREENING_DEFAULT_TAB },
        search: location.search,
      })
    }
  },
  component: LogScreeningPage,
})
