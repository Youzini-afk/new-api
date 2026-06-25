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
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Dices, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuotaWithCurrency } from '@/lib/currency'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Dialog } from '@/components/dialog'
import { Turnstile } from '@/components/turnstile'
import { getRouletteStatus, spinRoulette } from '../api'
import { ROULETTE_QUICK_STAKES } from '../constants'
import type {
  RouletteSpinResult,
  RouletteStatusData,
  RouletteWheelOutcome,
} from '../types'

interface RouletteCardProps {
  /** Whether the system-level roulette toggle is enabled (status.roulette_enabled). */
  rouletteEnabled: boolean
  /** Whether Turnstile verification is required for sensitive operations. */
  turnstileEnabled: boolean
  /** Turnstile site key (only used when turnstileEnabled is true). */
  turnstileSiteKey: string
}

/**
 * Generate a fresh idempotency key for a single spin attempt. The same key
 * must be reused across the initial request and the Turnstile-token retry
 * so the backend can de-duplicate a duplicate submission. We use
 * `crypto.randomUUID` when available and fall back to a timestamp-based
 * pseudo-random value for older environments.
 */
function makeIdempotencyKey(): string {
  try {
    if (
      typeof crypto !== 'undefined' &&
      typeof crypto.randomUUID === 'function'
    ) {
      return crypto.randomUUID()
    }
  } catch {
    /* fall through */
  }
  return `roulette-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}

/**
 * Format a basis-point multiplier as a human-readable label, e.g.
 * `10000` -> `1x`, `30000` -> `3x`, `0` -> `0x`.
 */
function formatMultiplier(bps: number): string {
  if (!Number.isFinite(bps)) return '-'
  return `${Math.round(bps / 100) / 100}x`
}

export function RouletteCard({
  rouletteEnabled,
  turnstileEnabled,
  turnstileSiteKey,
}: RouletteCardProps) {
  const { t } = useTranslation()
  const setUser = useAuthStore((s) => s.auth.setUser)
  const userId = useAuthStore((s) => s.auth.user?.id ?? 0)

  const [stakeQuota, setStakeQuota] = useState<number>(0)
  const [submitting, setSubmitting] = useState(false)
  const [turnstileModalVisible, setTurnstileModalVisible] = useState(false)
  const [turnstileWidgetKey, setTurnstileWidgetKey] = useState(0)
  // Stable per-click idempotency key. Replaced only when a brand-new spin
  // is started (not on Turnstile retries of the same attempt).
  const idempotencyKeyRef = useRef<string>('')
  // The latest completed spin result, surfaced in the result reveal panel.
  const [lastResult, setLastResult] = useState<RouletteSpinResult | null>(null)
  // Reveal animation toggle for the result panel.
  const [revealKey, setRevealKey] = useState(0)

  /* eslint-disable @tanstack/query/exhaustive-deps */
  const {
    data: status,
    isLoading,
    isError,
    refetch,
  } = useQuery({
    queryKey: ['roulette-status', userId],
    queryFn: async () => {
      const res = await getRouletteStatus()
      if (res.success && res.data) {
        return res.data
      }
      throw new Error(res.message || t('Failed to load roulette status'))
    },
    enabled: rouletteEnabled && userId > 0,
    staleTime: 30000,
  })
  /* eslint-enable @tanstack/query/exhaustive-deps */

  const stakeBounds = useMemo(() => {
    const minQuota = Number(status?.stake_min_quota ?? 0)
    const maxQuota = Number(status?.stake_max_quota ?? 0)
    return { minQuota, maxQuota }
  }, [status?.stake_min_quota, status?.stake_max_quota])

  // Keep the stake input inside the backend-reported bounds. Falls back to
  // the minimum on first load or when bounds shrink below the current value.
  useEffect(() => {
    setStakeQuota((current) => {
      const min = stakeBounds.minQuota || 0
      const max = stakeBounds.maxQuota || 0
      if (!Number.isFinite(current) || current < min) return min
      if (max > 0 && current > max) return max
      return current
    })
  }, [stakeBounds.minQuota, stakeBounds.maxQuota])

  const dailySpinCount = Number(status?.daily_spin_count ?? 0)
  const dailySpinLimit = Number(status?.daily_spin_limit ?? 0)
  const spinsDisabled = status ? dailySpinLimit <= 0 : false
  const limitReached =
    spinsDisabled || (dailySpinLimit > 0 && dailySpinCount >= dailySpinLimit)
  const eligible = status?.eligible !== false
  const stakeValid =
    Number.isFinite(stakeQuota) &&
    stakeQuota >= stakeBounds.minQuota &&
    (stakeBounds.maxQuota === 0 || stakeQuota <= stakeBounds.maxQuota)

  // Pre-compute outcome probabilities (weight / total weight) once per
  // status payload so the chips can show a stable percentage without
  // re-running the sum on every render.
  const wheelWithProb = useMemo(() => {
    const outcomes = status?.wheel ?? []
    const totalWeight = outcomes.reduce((sum, o) => sum + Number(o.weight || 0), 0)
    return outcomes.map((o) => ({
      outcome: o,
      probability: totalWeight > 0 ? Number(o.weight || 0) / totalWeight : 0,
    }))
  }, [status?.wheel])

  const shouldTriggerTurnstile = useCallback(
    (message?: string) => {
      if (!turnstileEnabled) return false
      if (typeof message !== 'string') return true
      return message.includes('Turnstile')
    },
    [turnstileEnabled]
  )

  const syncUserQuota = useCallback(
    (newQuota: number | undefined) => {
      if (newQuota === undefined) return
      const currentUser = useAuthStore.getState().auth.user
      if (!currentUser) return
      setUser({ ...currentUser, quota: newQuota })
    },
    [setUser]
  )

  const doSpin = useCallback(
    async (token?: string) => {
      if (!stakeValid) {
        toast.error(
          t('Stake must be between {{min}} and {{max}} quota', {
            min: stakeBounds.minQuota,
            max: stakeBounds.maxQuota || t('unlimited'),
          })
        )
        return
      }

      // Generate a fresh idempotency key only for a brand-new attempt (no
      // token yet). When retrying through Turnstile we reuse the key from
      // the original attempt so the backend can de-duplicate.
      if (!token) {
        idempotencyKeyRef.current = makeIdempotencyKey()
      }
      const idempotencyKey = idempotencyKeyRef.current
      if (!idempotencyKey) {
        toast.error(t('Spin failed'))
        return
      }

      setSubmitting(true)
      try {
        const res = await spinRoulette(
          { stake_quota: stakeQuota, idempotency_key: idempotencyKey },
          token
        )
        if (res.success && res.data) {
          const result = res.data
          setLastResult(result)
          setRevealKey((v) => v + 1)
          setTurnstileModalVisible(false)
          syncUserQuota(result.new_quota)
          if (result.capped && result.prize_quota > 0) {
            toast.success(
              t('You won {{amount}} (payout capped by user quota limit)', {
                amount: formatQuotaWithCurrency(result.prize_quota, {
                  digitsLarge: 0,
                }),
              })
            )
          } else if (result.capped) {
            toast.warning(t('Payout capped by user quota limit'))
          } else if (result.prize_quota > 0) {
            toast.success(
              t('You won {{amount}}', {
                amount: formatQuotaWithCurrency(result.prize_quota, {
                  digitsLarge: 0,
                }),
              })
            )
          } else {
            toast.info(t('No win this time'))
          }
          refetch()
        } else {
          if (!token && shouldTriggerTurnstile(res.message)) {
            if (!turnstileSiteKey) {
              toast.error(t('Turnstile is enabled but site key is empty.'))
              return
            }
            // Keep idempotencyKeyRef stable for the upcoming retry.
            setTurnstileModalVisible(true)
            return
          }
          if (token && shouldTriggerTurnstile(res.message)) {
            // Token rejected; reset widget so the user can verify again.
            setTurnstileWidgetKey((v) => v + 1)
          } else if (token) {
            // Token consumed but request failed for another reason; close
            // the modal and force a fresh key on the next attempt.
            setTurnstileModalVisible(false)
            setTurnstileWidgetKey((v) => v + 1)
            idempotencyKeyRef.current = ''
          }
          toast.error(res.message || t('Spin failed'))
        }
      } catch {
        if (token) {
          setTurnstileModalVisible(false)
          setTurnstileWidgetKey((v) => v + 1)
          idempotencyKeyRef.current = ''
        }
        toast.error(t('Spin failed'))
      } finally {
        setSubmitting(false)
      }
    },
    [
      refetch,
      shouldTriggerTurnstile,
      stakeBounds.maxQuota,
      stakeBounds.minQuota,
      stakeQuota,
      stakeValid,
      syncUserQuota,
      t,
      turnstileSiteKey,
    ]
  )

  const handleTurnstileVerify = useCallback(
    (token: string) => {
      doSpin(token)
    },
    [doSpin]
  )

  const handleTurnstileExpire = useCallback(() => {
    setTurnstileWidgetKey((v) => v + 1)
  }, [])

  if (!rouletteEnabled) return null

  // When the backend reports the feature disabled, hide the card entirely.
  if (status && !status.enabled) return null

  if (isLoading) {
    return (
      <Card data-card-hover='false' className='gap-0 overflow-hidden py-0'>
        <div className='p-6'>
          <div className='flex items-start gap-3'>
            <Skeleton className='h-10 w-10 rounded-xl' />
            <div className='space-y-2'>
              <Skeleton className='h-5 w-40' />
              <Skeleton className='h-3 w-56' />
            </div>
          </div>
        </div>
      </Card>
    )
  }

  if (isError || !status) {
    return (
      <Card data-card-hover='false' className='gap-0 overflow-hidden py-0'>
        <div className='flex flex-col items-center justify-center gap-3 p-8 text-center'>
          <p className='text-destructive text-sm'>
            {t('Failed to load roulette status')}
          </p>
          <Button
            variant='outline'
            size='sm'
            onClick={() => refetch()}
            disabled={submitting}
          >
            <RefreshCw className='h-3.5 w-3.5' />
            {t('Retry')}
          </Button>
        </div>
      </Card>
    )
  }

  const dailyStakeQuota = Number(status.daily_stake_quota ?? 0)
  const dailyStakeLimit = Number(status.daily_stake_limit ?? 0)
  const rtpPct =
    Number.isFinite(status.rtp_bps) ? status.rtp_bps / 100 : 0
  const dailyStakeExceeded =
    dailyStakeLimit > 0 && dailyStakeQuota + stakeQuota > dailyStakeLimit

  const spinReady =
    stakeValid && eligible && !limitReached && !dailyStakeExceeded && stakeQuota > 0

  function resolveSpinButtonLabel() {
    if (submitting || turnstileModalVisible) return t('Processing...')
    if (spinsDisabled) return t('Spins disabled')
    if (!eligible) return t('Not eligible')
    if (limitReached) return t('Daily limit reached')
    if (dailyStakeExceeded) return t('Daily stake cap reached')
    return t('Spin')
  }
  const spinButtonLabel = resolveSpinButtonLabel()

  const winOutcome = lastResult
    ? status.wheel.find((o) => o.key === lastResult.outcome_key)
    : undefined

  return (
    <>
      <Dialog
        open={turnstileModalVisible}
        onOpenChange={(open) => {
          setTurnstileModalVisible(open)
          if (!open) {
            // Modal closed (user dismissed or terminal failure): reset the
            // widget and clear the pending idempotency key so the next
            // attempt starts fresh.
            setTurnstileWidgetKey((v) => v + 1)
            idempotencyKeyRef.current = ''
          }
        }}
        title={t('Security Check')}
        contentClassName='sm:max-w-md'
        contentHeight='auto'
        bodyClassName='space-y-4'
      >
        <div className='text-muted-foreground text-sm'>
          {t('Please complete the security check to continue.')}
        </div>
        <div className='flex justify-center py-4'>
          <Turnstile
            key={turnstileWidgetKey}
            siteKey={turnstileSiteKey}
            onVerify={handleTurnstileVerify}
            onExpire={handleTurnstileExpire}
          />
        </div>
      </Dialog>

      <Card data-card-hover='false' className='gap-0 overflow-hidden py-0'>
        {/* Header */}
        <div className='border-b p-4 sm:p-6'>
          <div className='flex items-start gap-3'>
            <div className='bg-violet-500/10 text-violet-600 dark:text-violet-400 flex h-10 w-10 shrink-0 items-center justify-center rounded-xl sm:h-11 sm:w-11'>
              <Dices className='h-4 w-4 sm:h-5 sm:w-5' strokeWidth={2} />
            </div>
            <div className='min-w-0 flex-1'>
              <h3 className='text-base font-semibold tracking-tight sm:text-lg'>
                {t('Quota Roulette')}
              </h3>
              <p className='text-muted-foreground mt-1 text-xs sm:text-sm'>
                {t(
                  'Stake is deducted upfront; spin resolves instantly and pays out by multiplier. Wheel RTP is operator-capped and rate-limited.'
                )}
              </p>
            </div>
          </div>
        </div>

        {/* Stats */}
        <div className='grid grid-cols-1 gap-px border-b sm:grid-cols-3'>
          <div className='bg-card p-3 text-center sm:p-5'>
            <div className='text-xl font-semibold tracking-tight tabular-nums sm:text-2xl'>
              {spinsDisabled ? '—' : dailySpinCount}
              {!spinsDisabled && (
                <span className='text-muted-foreground'>
                  /{dailySpinLimit}
                </span>
              )}
            </div>
            <div className='text-muted-foreground mt-0.5 text-[10px] font-medium sm:mt-1 sm:text-xs'>
              {t('Today spins')}
            </div>
          </div>
          <div className='bg-card p-3 text-center sm:p-5'>
            <div className='text-sm font-semibold tracking-tight tabular-nums sm:text-lg'>
              {formatQuotaWithCurrency(dailyStakeQuota, { digitsLarge: 0 })}
              <span className='text-muted-foreground'>
                {dailyStakeLimit > 0
                  ? `/${formatQuotaWithCurrency(dailyStakeLimit, {
                      digitsLarge: 0,
                    })}`
                  : ''}
              </span>
            </div>
            <div className='text-muted-foreground mt-0.5 text-[10px] font-medium sm:mt-1 sm:text-xs'>
              {t('Staked today')}
            </div>
          </div>
          <div className='bg-card p-3 text-center sm:p-5'>
            <div className='text-xl font-semibold tracking-tight tabular-nums sm:text-2xl'>
              {formatQuotaWithCurrency(status.current_quota, {
                digitsLarge: 0,
              })}
            </div>
            <div className='text-muted-foreground mt-0.5 text-[10px] font-medium sm:mt-1 sm:text-xs'>
              {t('Your quota')}
            </div>
          </div>
        </div>

        <div className='space-y-4 p-4 sm:space-y-5 sm:p-6'>
          {/* RTP / safety callout */}
          <div className='bg-muted/30 rounded-lg border p-3 text-xs sm:text-sm'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <span className='text-muted-foreground'>
                {t('Wheel RTP')}
              </span>
              <span className='font-medium tabular-nums'>
                {rtpPct.toFixed(2)}%
              </span>
            </div>
            <div className='text-muted-foreground mt-1.5 text-[11px] leading-relaxed sm:text-xs'>
              {t(
                'Each spin deducts stake first; the configured wheel pays out by multiplier. Long-run wheel returns must stay at or below the operator RTP cap; individual payouts may be reduced only by the user quota cap.'
              )}
            </div>
          </div>

          {/* Ineligible banner */}
          {!eligible && status.ineligible_reason && (
            <div className='border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200 rounded-lg border px-3 py-2 text-xs sm:text-sm'>
              {status.ineligible_reason}
            </div>
          )}

          {/* Wheel outcomes */}
          {wheelWithProb.length > 0 && (
            <div>
              <div className='text-muted-foreground mb-1.5 text-xs font-medium'>
                {t('Wheel outcomes')}
              </div>
              <div className='flex flex-wrap gap-1.5 sm:gap-2'>
                {wheelWithProb.map(({ outcome, probability }) => (
                  <OutcomeChip
                    key={outcome.key}
                    outcome={outcome}
                    probability={probability}
                    highlight={
                      lastResult?.outcome_key === outcome.key
                    }
                  />
                ))}
              </div>
              <div className='text-muted-foreground mt-1.5 text-[11px] sm:text-xs'>
                {t('Multiplier is total payout (1x = stake returned, break-even).')}
              </div>
            </div>
          )}

          {/* Result reveal */}
          {lastResult && (
            <ResultReveal
              key={revealKey}
              result={lastResult}
              outcome={winOutcome}
            />
          )}

          {/* Recent spins */}
          {status.my_recent_spins.length > 0 && (
            <RecentSpinsList spins={status.my_recent_spins} />
          )}

          {/* Stake + spin */}
          <div className='border-t pt-4'>
            <div className='text-muted-foreground mb-2 text-xs font-medium'>
              {t('Stake amount (quota)')}
            </div>
            <div className='mb-2 flex flex-wrap items-center gap-2'>
              {ROULETTE_QUICK_STAKES.map((s) => {
                const disabled =
                  (stakeBounds.minQuota > 0 && s < stakeBounds.minQuota) ||
                  (stakeBounds.maxQuota > 0 && s > stakeBounds.maxQuota)
                return (
                  <Button
                    key={`stake-${s}`}
                    type='button'
                    size='sm'
                    variant={stakeQuota === s ? 'default' : 'outline'}
                    onClick={() => setStakeQuota(s)}
                    disabled={disabled}
                    className='tabular-nums'
                  >
                    {formatQuotaWithCurrency(s, { digitsLarge: 0 })}
                  </Button>
                )
              })}
              <input
                type='number'
                min={stakeBounds.minQuota || undefined}
                max={stakeBounds.maxQuota || undefined}
                step={1}
                inputMode='numeric'
                value={stakeQuota || ''}
                onChange={(e) => {
                  const next = Number(e.target.value)
                  setStakeQuota(Number.isFinite(next) ? Math.floor(next) : 0)
                }}
                className='bg-background border-input focus-visible:ring-ring h-9 w-32 rounded-md border px-2 text-sm tabular-nums outline-none focus-visible:ring-2'
                aria-label={t('Stake amount (quota)')}
              />
            </div>
            <div className='text-muted-foreground mb-3 text-[11px] sm:text-xs'>
              {t('Approximate value')}: {formatQuotaWithCurrency(stakeQuota, {
                digitsLarge: 0,
              })}
            </div>
            {!stakeValid && (
              <div className='text-destructive mb-3 text-xs'>
                {t('Stake must be between {{min}} and {{max}} quota', {
                  min: stakeBounds.minQuota,
                  max: stakeBounds.maxQuota || t('unlimited'),
                })}
              </div>
            )}
            {dailyStakeExceeded && (
              <div className='text-destructive mb-3 text-xs'>
                {t('This stake would exceed your daily roulette stake cap')}
              </div>
            )}

            <div className='flex flex-wrap items-center justify-between gap-2'>
              <div className='text-muted-foreground text-xs'>
                {stakeValid && stakeQuota > 0
                  ? `${t('Stake')}: ${formatQuotaWithCurrency(stakeQuota, {
                      digitsLarge: 0,
                    })}`
                  : t('Enter a stake to spin')}
              </div>
              <Button
                onClick={() => doSpin()}
                disabled={
                  submitting || turnstileModalVisible || !spinReady
                }
                size='sm'
                className='w-full sm:w-auto'
              >
                {spinButtonLabel}
              </Button>
            </div>
          </div>
        </div>
      </Card>
    </>
  )
}

// ---------------------------------------------------------------------------
// Sub-components (kept local to avoid file sprawl; each renders a focused
// slice of the status payload).
// ---------------------------------------------------------------------------

function OutcomeChip({
  outcome,
  probability,
  highlight,
}: {
  outcome: RouletteWheelOutcome
  probability: number
  highlight: boolean
}) {
  const isWin = outcome.multiplier_bps > 0
  return (
    <div
      className={cn(
        'flex min-w-[72px] flex-col items-center rounded-lg border px-2.5 py-1.5 text-center transition-colors',
        highlight &&
          'border-violet-400 bg-violet-500/10 dark:border-violet-600',
        !highlight &&
          isWin &&
          'border-emerald-200 bg-emerald-50/50 dark:border-emerald-900/60 dark:bg-emerald-950/20',
        !highlight &&
          !isWin &&
          'border-muted-foreground/20 bg-muted/30'
      )}
    >
      <span className='text-xs font-semibold tabular-nums sm:text-sm'>
        {formatMultiplier(outcome.multiplier_bps)}
      </span>
      <span className='text-muted-foreground mt-0.5 text-[10px] tabular-nums sm:text-[11px]'>
        {(probability * 100).toFixed(1)}%
      </span>
    </div>
  )
}

function ResultReveal({
  result,
  outcome,
}: {
  result: RouletteSpinResult
  outcome?: RouletteWheelOutcome
}) {
  const { t } = useTranslation()
  const won = result.prize_quota > 0
  const netPositive = result.net_quota > 0
  return (
    <div
      className={cn(
        'animate-in fade-in-0 zoom-in-95 rounded-lg border p-3 duration-300',
        won
          ? 'border-emerald-300 bg-emerald-50 dark:border-emerald-700 dark:bg-emerald-950/30'
          : 'border-muted-foreground/20 bg-muted/30'
      )}
    >
      <div className='flex items-center justify-between gap-2'>
        <div className='text-xs font-medium sm:text-sm'>
          {outcome ? formatMultiplier(outcome.multiplier_bps) : result.outcome_key}
          {result.capped && (
            <span
              className='text-amber-600 dark:text-amber-400 ml-2 text-[10px] uppercase tracking-wide sm:text-[11px]'
              title={t('Payout capped by user quota limit')}
              aria-label={t('Payout capped by user quota limit')}
            >
              {t('Quota capped')}
            </span>
          )}
        </div>
        <div
          className={cn(
            'text-xs font-semibold tabular-nums sm:text-sm',
            netPositive
              ? 'text-emerald-600 dark:text-emerald-400'
              : 'text-muted-foreground'
          )}
        >
          {netPositive ? '+' : ''}
          {formatQuotaWithCurrency(result.net_quota, { digitsLarge: 0 })}
        </div>
      </div>
      <div className='text-muted-foreground mt-2 space-y-1 text-[11px] tabular-nums sm:text-xs'>
        <div className='flex justify-between'>
          <span>{t('Stake')}</span>
          <span>
            {formatQuotaWithCurrency(result.stake_quota, { digitsLarge: 0 })}
          </span>
        </div>
        <div className='flex justify-between'>
          <span>{t('Prize')}</span>
          <span>
            {formatQuotaWithCurrency(result.prize_quota, { digitsLarge: 0 })}
          </span>
        </div>
        <div className='flex justify-between'>
          <span>{t('Spin day')}</span>
          <span>
            {t('Day')} {result.day}
          </span>
        </div>
      </div>
    </div>
  )
}

function RecentSpinsList({
  spins,
}: {
  spins: RouletteStatusData['my_recent_spins']
}) {
  const { t } = useTranslation()
  return (
    <div>
      <div className='text-muted-foreground mb-1.5 text-xs font-medium'>
        {t('Your recent spins')}
      </div>
      <div className='flex flex-col gap-1.5'>
        {spins.map((spin) => {
          const netPositive = spin.net_quota > 0
          return (
            <div
              key={spin.id}
              className='flex flex-wrap items-center justify-between gap-1.5 text-xs sm:text-sm'
            >
              <span className='text-muted-foreground tabular-nums'>
                {dayjs(spin.created_at * 1000).format('MM-DD HH:mm')}:
              </span>
              <span className='text-muted-foreground tabular-nums'>
                {t('Stake')}{' '}
                {formatQuotaWithCurrency(spin.stake_quota, {
                  digitsLarge: 0,
                })}
              </span>
              <span
                className={cn(
                  'tabular-nums',
                  netPositive
                    ? 'text-emerald-600 dark:text-emerald-400 font-medium'
                    : 'text-muted-foreground'
                )}
              >
                {netPositive ? '+' : ''}
                {formatQuotaWithCurrency(spin.net_quota, { digitsLarge: 0 })}
              </span>
              {spin.capped && (
                <span
                  className='text-amber-600 dark:text-amber-400 text-[10px] uppercase tracking-wide sm:text-[11px]'
                  title={t('Payout capped by user quota limit')}
                  aria-label={t('Payout capped by user quota limit')}
                >
                  {t('Quota capped')}
                </span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
