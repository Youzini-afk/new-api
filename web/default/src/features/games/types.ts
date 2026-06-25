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
// ============================================================================
// Lottery Type Definitions
//
// All response shapes mirror the Go backend DTOs in `model/lottery.go` and
// use snake_case keys to match the wire format exactly.
// ============================================================================

/**
 * Generic API response wrapper (mirrors profile feature convention).
 */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

/**
 * A single lottery ticket view (current or historical).
 */
export interface LotteryTicketView {
  id: number
  round_id: number
  red_balls: number[]
  blue_ball: number
  stake_quota: number
  prize_quota: number
  result: string
  created_at: number
  drawn_at: number
}

/**
 * A drawn round view shown under "recent rounds".
 */
export interface LotteryRoundView {
  day: number
  red_balls: number[]
  blue_ball: number
  drawn_at: number
  total_stake_quota: number
  pool_carry_in_quota: number
  pool_injected_quota: number
  pool_prize_quota: number
  pool_carry_out_quota: number
  winner_jackpot: number
  winner_second: number
  winner_third: number
  winner_fourth: number
  winner_fifth: number
  winner_small: number
}

/**
 * Full lottery status payload returned by `GET /api/user/game/lottery`.
 *
 * When the feature is disabled the backend still returns `success` with a
 * minimal payload where `enabled=false`; gate the UI on that flag.
 */
export interface LotteryStatusData {
  enabled: boolean
  eligible: boolean
  ineligible_reason: string
  round_id: number
  day: number
  status: string
  draw_at: number
  daily_buy_count: number
  daily_buy_limit: number
  current_quota: number
  stake_min_quota: number
  stake_max_quota: number
  red_ball_max: number
  blue_ball_max: number
  system_injected_quota: number
  pool_carry_in_quota: number
  pool_injected_quota: number
  total_stake_quota: number
  pool_prize_quota: number
  pool_carry_out_quota: number
  winner_jackpot: number
  winner_second: number
  winner_third: number
  winner_fourth: number
  winner_fifth: number
  winner_small: number
  my_tickets: LotteryTicketView[]
  my_recent_tickets: LotteryTicketView[]
  recent_rounds: LotteryRoundView[]
}

/**
 * Buy request body. `stake_usd` is preferred for UX (integer USD); the
 * backend converts it to quota using the system `QuotaPerUnit`.
 */
export interface LotteryBuyRequest {
  red_balls: number[]
  blue_ball: number
  stake_usd?: number
  stake_quota?: number
}

/**
 * Buy response payload returned on success.
 */
export interface LotteryBuyResult {
  round_id: number
  day: number
  status: string
  daily_buy_count: number
  daily_buy_limit: number
  new_quota: number
  ticket_id: number
  stake_quota: number
}
