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
import { api } from '@/lib/api'
import type {
  ApiResponse,
  LotteryBuyRequest,
  LotteryBuyResult,
  LotteryStatusData,
} from './types'

/**
 * Get current lottery status for the authenticated user.
 *
 * `GET /api/user/game/lottery`
 *
 * When the lottery feature is disabled the backend returns `success` with a
 * minimal payload (`data.enabled=false`); callers should gate the UI on that
 * flag rather than treating it as an error.
 */
export async function getLotteryStatus(): Promise<
  ApiResponse<LotteryStatusData>
> {
  const res = await api.get('/api/user/game/lottery')
  return res.data
}

/**
 * Buy a lottery ticket for the current round.
 *
 * `POST /api/user/game/lottery/buy`
 *
 * Prefer `stake_usd` (integer USD) for user-facing input; the backend
 * converts it to quota using the system `QuotaPerUnit`. When Turnstile is
 * enabled and the session has not yet been verified, pass a Turnstile token
 * via the `turnstileToken` parameter (sent as `?turnstile=...`).
 */
export async function buyLotteryTicket(
  data: LotteryBuyRequest,
  turnstileToken?: string
): Promise<ApiResponse<LotteryBuyResult>> {
  const url = turnstileToken
    ? `/api/user/game/lottery/buy?turnstile=${encodeURIComponent(
        turnstileToken
      )}`
    : '/api/user/game/lottery/buy'
  const res = await api.post(url, data)
  return res.data
}
