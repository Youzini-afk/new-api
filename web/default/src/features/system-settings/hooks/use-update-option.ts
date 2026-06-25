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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import { toast } from 'sonner'
import { updateSystemOption } from '../api'
import type { UpdateOptionRequest } from '../types'

// Configuration keys that require status refresh
const STATUS_RELATED_KEYS = [
  'theme.frontend',
  'HeaderNavModules',
  'SidebarModulesAdmin',
  'Notice',
  'LogConsumeEnabled',
  'QuotaPerUnit',
  'USDExchangeRate',
  'DisplayInCurrencyEnabled',
  'DisplayTokenStatEnabled',
  'general_setting.quota_display_type',
  'general_setting.custom_currency_symbol',
  'general_setting.custom_currency_exchange_rate',
  // Feature toggles exposed via /api/status (drive profile-page gating).
  'checkin_setting.enabled',
  'game_setting.lottery_enabled',
  'game_setting.roulette_enabled',
]

const LOTTERY_STATUS_RELATED_KEYS = [
  'QuotaPerUnit',
  'USDExchangeRate',
  'DisplayInCurrencyEnabled',
  'general_setting.quota_display_type',
  'general_setting.custom_currency_symbol',
  'general_setting.custom_currency_exchange_rate',
  'game_setting.lottery_enabled',
  'game_setting.lottery_daily_buy_limit',
  'game_setting.lottery_min_stake_quota',
  'game_setting.lottery_max_stake_quota',
  'game_setting.lottery_system_injected_quota',
  'game_setting.lottery_max_user_quota',
  'game_setting.lottery_draw_hour',
]

const ROULETTE_STATUS_RELATED_KEYS = [
  'QuotaPerUnit',
  'USDExchangeRate',
  'DisplayInCurrencyEnabled',
  'general_setting.quota_display_type',
  'general_setting.custom_currency_symbol',
  'general_setting.custom_currency_exchange_rate',
  'game_setting.roulette_enabled',
  'game_setting.roulette_daily_spin_limit',
  'game_setting.roulette_min_stake_quota',
  'game_setting.roulette_max_stake_quota',
  'game_setting.roulette_max_daily_stake_quota',
  'game_setting.roulette_max_user_quota',
  'game_setting.roulette_rtp_bps',
  'game_setting.roulette_wheel',
]

export function useUpdateOption() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (request: UpdateOptionRequest) => updateSystemOption(request),
    onSuccess: (data, variables) => {
      if (data.success) {
        // Always refresh system-options
        queryClient.invalidateQueries({ queryKey: ['system-options'] })

        // If updating frontend-display-related config, also refresh status
        if (STATUS_RELATED_KEYS.includes(variables.key)) {
          queryClient.invalidateQueries({ queryKey: ['status'] })
          try {
            window.localStorage.removeItem('status')
          } catch {
            /* empty */
          }
        }

        if (LOTTERY_STATUS_RELATED_KEYS.includes(variables.key)) {
          queryClient.invalidateQueries({ queryKey: ['lottery-status'] })
        }

        if (ROULETTE_STATUS_RELATED_KEYS.includes(variables.key)) {
          queryClient.invalidateQueries({ queryKey: ['roulette-status'] })
        }

        toast.success(i18next.t('Setting updated successfully'))
      } else {
        toast.error(data.message || i18next.t('Failed to update setting'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('Failed to update setting'))
    },
  })
}
