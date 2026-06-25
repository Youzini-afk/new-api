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
import { SettingsPage } from '../components/settings-page'
import type { GovernanceSettings } from '../types'
import {
  GOVERNANCE_DEFAULT_SECTION,
  getGovernanceSectionContent,
  getGovernanceSectionMeta,
} from './section-registry.tsx'

const defaultGovernanceSettings: GovernanceSettings = {
  relay_error_governance: '',
}

export function GovernanceSettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/governance/$section'
      defaultSettings={defaultGovernanceSettings}
      defaultSection={GOVERNANCE_DEFAULT_SECTION}
      getSectionContent={getGovernanceSectionContent}
      getSectionMeta={getGovernanceSectionMeta}
    />
  )
}
