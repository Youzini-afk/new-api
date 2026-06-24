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
 * Prompt Blocks tab — thin wrapper over the shared BlockLogsTab.
 */
import { BlockLogsTab } from './block-logs-tab'
import {
  appendPromptBlockRemark,
  getPromptBlockLogDetail,
  getPromptBlockLogs,
} from '../api'

export function PromptBlocksTab() {
  return (
    <BlockLogsTab
      kind='prompt'
      queryKeyPrefix='prompt-blocks'
      list={(params) => getPromptBlockLogs(params)}
      detail={(id) => getPromptBlockLogDetail(id)}
      remark={(id, remark) => appendPromptBlockRemark(id, remark)}
    />
  )
}
