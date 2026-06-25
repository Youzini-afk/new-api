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
import { ErrorGovernanceSection } from './error-governance-section'
import type { GovernanceSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'

const GOVERNANCE_SECTIONS = [
  {
    id: 'error-governance',
    titleKey: 'Relay Error Governance',
    build: (settings: GovernanceSettings) => (
      <ErrorGovernanceSection
        defaultValue={settings.relay_error_governance ?? ''}
      />
    ),
  },
] as const

export type GovernanceSectionId = (typeof GOVERNANCE_SECTIONS)[number]['id']

const governanceRegistry = createSectionRegistry<
  GovernanceSectionId,
  GovernanceSettings
>({
  sections: GOVERNANCE_SECTIONS,
  defaultSection: 'error-governance',
  basePath: '/system-settings/governance',
  urlStyle: 'path',
})

export const GOVERNANCE_SECTION_IDS = governanceRegistry.sectionIds
export const GOVERNANCE_DEFAULT_SECTION = governanceRegistry.defaultSection
export const getGovernanceSectionNavItems = governanceRegistry.getSectionNavItems
export const getGovernanceSectionContent = governanceRegistry.getSectionContent
export const getGovernanceSectionMeta = governanceRegistry.getSectionMeta
