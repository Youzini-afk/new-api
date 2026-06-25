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
import i18next from 'i18next'
import {
  PAYMENT_TYPES,
  DEFAULT_PRESET_MULTIPLIERS,
  DEFAULT_PAYMENT_TYPE,
  DEFAULT_MIN_TOPUP,
} from '../constants'
import type { PresetAmount, TopupInfo } from '../types'

// ============================================================================
// Payment Processing Functions
// ============================================================================

/**
 * Reserved hosted-provider types that must never appear in the standard
 * Epay PayMethods list. They are surfaced through dedicated hooks/sections and
 * gated by their own enable flags (Stripe / Waffo Pancake) or product flows
 * (Creem), not by the shared PayMethods list. Matched case-insensitively.
 */
const RESERVED_HOSTED_PROVIDER_TYPES: ReadonlySet<string> = new Set([
  PAYMENT_TYPES.STRIPE,
  PAYMENT_TYPES.CREEM,
  PAYMENT_TYPES.WAFFO,
  PAYMENT_TYPES.WAFFO_PANCAKE,
])

/**
 * Returns true when `type` is one of the reserved hosted-provider types
 * (stripe / creem / waffo / waffo_pancake), matched case-insensitively.
 */
export function isReservedHostedProviderType(type: string): boolean {
  if (!type) return false
  return RESERVED_HOSTED_PROVIDER_TYPES.has(type.trim().toLowerCase())
}

/**
 * Reject non-navigable schemes (e.g. javascript:, data:) and relative URLs.
 * Only http/https are allowed for backend-provided redirect targets.
 */
export function isSafeHttpCheckoutUrl(value: string): boolean {
  const trimmed = value.trim()
  if (!trimmed) return false
  try {
    const u = new URL(trimmed)
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch {
    return false
  }
}

/**
 * Extract a user-facing error message from a payment API response.
 *
 * Backend failure shapes vary across providers:
 *   - `{ message: "error", data: "<real reason>" }` (Epay / Waffo / Pancake)
 *   - `{ success: false, message: "<real reason>" }` (Stripe / Creem)
 *   - `{ data: "<reason>" }` with no usable message field
 *
 * Prefer a non-empty string `data`; otherwise fall back to `message` (unless
 * it is the literal placeholder `error` or the success marker `success`); and
 * finally fall back to the localized default.
 */
export function extractPaymentError(
  response: { message?: string; data?: unknown } | null | undefined,
  fallbackKey: string = 'Payment request failed'
): string {
  if (response) {
    const data = response.data
    if (typeof data === 'string' && data.trim()) {
      return data
    }

    const message = response.message
    if (message && message !== 'success' && message !== 'error') {
      return message
    }
  }

  return i18next.t(fallbackKey)
}

/**
 * Check if browser is Safari
 */
function isSafariBrowser(): boolean {
  return (
    navigator.userAgent.indexOf('Safari') > -1 &&
    navigator.userAgent.indexOf('Chrome') < 1
  )
}

/**
 * Submit payment form (for non-Stripe payments)
 */
export function submitPaymentForm(
  url: string,
  params: Record<string, unknown>
): void {
  const form = document.createElement('form')
  form.action = url
  form.method = 'POST'

  // Don't open in new tab for Safari
  if (!isSafariBrowser()) {
    form.target = '_blank'
  }

  // Add form parameters
  Object.entries(params).forEach(([key, value]) => {
    const input = document.createElement('input')
    input.type = 'hidden'
    input.name = key
    input.value = String(value)
    form.appendChild(input)
  })

  document.body.appendChild(form)
  form.submit()
  document.body.removeChild(form)
}

/**
 * Check if payment method is Stripe
 */
export function isStripePayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.STRIPE
}

/**
 * Check if payment method is Waffo Pancake
 *
 * Pancake is a metered-style payment that goes through a dedicated checkout
 * URL flow rather than the generic epay form submission, so it must be
 * special-cased in payment dispatch logic.
 */
export function isWaffoPancakePayment(paymentType: string): boolean {
  return paymentType === PAYMENT_TYPES.WAFFO_PANCAKE
}

/**
 * Get default payment type from topup info.
 *
 * The standard PayMethods list is expected to only contain Epay methods
 * (hosted providers like stripe / creem / waffo_pancake are filtered out by
 * `parsePaymentMethods` in `use-topup-info.ts`). Defensive guard here rejects
 * any reserved hosted type that slipped through. Hosted-provider buttons are
 * rendered as dedicated actions, so the default must never silently select one.
 */
export function getDefaultPaymentType(topupInfo: TopupInfo | null): string {
  if (!topupInfo) {
    return DEFAULT_PAYMENT_TYPE
  }

  // Return first available Epay payment method or default
  if (topupInfo.pay_methods?.length > 0) {
    const firstEpay = topupInfo.pay_methods.find(
      (m) => m.type && !isReservedHostedProviderType(m.type)
    )
    if (firstEpay) {
      return firstEpay.type
    }
  }

  return DEFAULT_PAYMENT_TYPE
}

/**
 * Get minimum topup amount from topup info
 */
export function getMinTopupAmount(topupInfo: TopupInfo | null): number {
  if (!topupInfo) {
    return DEFAULT_MIN_TOPUP
  }

  if (topupInfo.enable_online_topup) {
    return topupInfo.min_topup
  }

  if (topupInfo.enable_stripe_topup) {
    return topupInfo.stripe_min_topup
  }

  if (topupInfo.enable_waffo_topup) {
    return topupInfo.waffo_min_topup || DEFAULT_MIN_TOPUP
  }

  if (topupInfo.enable_waffo_pancake_topup) {
    return topupInfo.waffo_pancake_min_topup || DEFAULT_MIN_TOPUP
  }

  return DEFAULT_MIN_TOPUP
}

/**
 * Generate preset amounts based on minimum topup
 */
export function generatePresetAmounts(minAmount: number): PresetAmount[] {
  return DEFAULT_PRESET_MULTIPLIERS.map((multiplier) => ({
    value: minAmount * multiplier,
  }))
}

/**
 * Merge custom preset amounts with discounts
 */
export function mergePresetAmounts(
  amountOptions: number[],
  discounts: Record<number, number>
): PresetAmount[] {
  if (!amountOptions || amountOptions.length === 0) {
    return []
  }

  return amountOptions.map((amount) => ({
    value: amount,
    discount: discounts[amount] || 1.0,
  }))
}
