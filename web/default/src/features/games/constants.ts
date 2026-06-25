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
// Lottery constants
// ============================================================================

/**
 * Default ball grid sizes; the backend can override these per-round via
 * `red_ball_max` / `blue_ball_max` in the status payload.
 */
export const LOTTERY_DEFAULT_RED_BALL_MAX = 12
export const LOTTERY_DEFAULT_BLUE_BALL_MAX = 6
export const LOTTERY_RED_BALLS_REQUIRED = 5

/**
 * Quick-pick stake chips shown next to the stake input (integer USD).
 */
export const LOTTERY_QUICK_STAKES = [1, 10, 50, 100] as const

/**
 * Map a wire-tier string (emitted by `determinePrizeTier` in
 * `model/lottery.go`; `pending` is used for tickets in the current open
 * round that have not been drawn yet) to the i18n key used for display.
 *
 * The returned key is an English source string passed through `t()`; locales
 * translate it.
 */
export function tierLabelKey(tier: string): string {
  switch (tier) {
    case 'jackpot':
      return 'Jackpot'
    case 'second':
      return 'Second prize'
    case 'third':
      return 'Third prize'
    case 'fourth':
      return 'Fourth prize'
    case 'fifth':
      return 'Fifth prize'
    case 'small_three':
      return 'Small prize (3 red)'
    case 'small_two_blue':
      return 'Small prize (2 red + blue)'
    case 'small_one_blue':
      return 'Small prize (1 red + blue)'
    case 'none':
      return 'No prize'
    case 'pending':
      return 'Pending draw'
    default:
      return tier
  }
}

// ============================================================================
// Roulette constants
// ============================================================================

/**
 * Maximum allowed RTP in basis points. The backend enforces a hard cap of
 * 9500 (95%); admin validation mirrors that so operators cannot save an
 * unsafe value through the form.
 */
export const ROULETTE_MAX_RTP_BPS = 9500

/**
 * Default wheel config used to prefill the admin settings textarea when the
 * operator has not configured one yet. Uses the same shape/weights as the
 * backend default but with double-quoted JSON for safe `JSON.parse`.
 */
export const ROULETTE_DEFAULT_WHEEL_JSON =
  '[{"key":"lose","multiplier_bps":0,"weight":50},{"key":"x1","multiplier_bps":10000,"weight":20},{"key":"x2","multiplier_bps":20000,"weight":20},{"key":"x3","multiplier_bps":30000,"weight":10}]'

/**
 * Quick-pick stake chips shown next to the stake input (raw quota units).
 * These are conservative placeholders; the real bounds come from the
 * backend status payload.
 */
export const ROULETTE_QUICK_STAKES = [500000, 2500000, 5000000] as const
