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
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Ticket, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuotaWithCurrency, getCurrencyDisplay } from '@/lib/currency'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Dialog } from '@/components/dialog'
import { Turnstile } from '@/components/turnstile'
import {
  buyLotteryTicket,
  getLotteryStatus,
} from '../api'
import {
  LOTTERY_DEFAULT_BLUE_BALL_MAX,
  LOTTERY_DEFAULT_RED_BALL_MAX,
  LOTTERY_QUICK_STAKES,
  LOTTERY_RED_BALLS_REQUIRED,
  tierLabelKey,
} from '../constants'
import type {
  LotteryRoundView,
  LotteryTicketView,
} from '../types'

interface LotteryCardProps {
  /** Whether the system-level lottery toggle is enabled (status.lottery_enabled). */
  lotteryEnabled: boolean
  /** Whether Turnstile verification is required for sensitive operations. */
  turnstileEnabled: boolean
  /** Turnstile site key (only used when turnstileEnabled is true). */
  turnstileSiteKey: string
}

const DRAW_AHEAD_PRE_SALE_MS = 12 * 60 * 60 * 1000

export function LotteryCard({
  lotteryEnabled,
  turnstileEnabled,
  turnstileSiteKey,
}: LotteryCardProps) {
  const { t } = useTranslation()
  const setUser = useAuthStore((s) => s.auth.setUser)
  const userId = useAuthStore((s) => s.auth.user?.id ?? 0)

  const [selectedRed, setSelectedRed] = useState<number[]>([])
  const [selectedBlue, setSelectedBlue] = useState<number | null>(null)
  const [stakeUsd, setStakeUsd] = useState<number>(1)
  const [submitting, setSubmitting] = useState(false)
  const [turnstileModalVisible, setTurnstileModalVisible] = useState(false)
  const [turnstileWidgetKey, setTurnstileWidgetKey] = useState(0)

  /* eslint-disable @tanstack/query/exhaustive-deps */
  const {
    data: status,
    isLoading,
    isError,
    refetch,
  } = useQuery({
    queryKey: ['lottery-status', userId],
    queryFn: async () => {
      const res = await getLotteryStatus()
      if (res.success && res.data) {
        return res.data
      }
      throw new Error(res.message || t('Failed to load lottery status'))
    },
    enabled: lotteryEnabled && userId > 0,
    staleTime: 30000,
  })
  /* eslint-enable @tanstack/query/exhaustive-deps */

  const quotaPerUnit = getCurrencyDisplay().config.quotaPerUnit

  const redBallMax = status?.red_ball_max ?? LOTTERY_DEFAULT_RED_BALL_MAX
  const blueBallMax = status?.blue_ball_max ?? LOTTERY_DEFAULT_BLUE_BALL_MAX
  const redOptions = useMemo(
    () => Array.from({ length: redBallMax }, (_, i) => i + 1),
    [redBallMax]
  )
  const blueOptions = useMemo(
    () => Array.from({ length: blueBallMax }, (_, i) => i + 1),
    [blueBallMax]
  )

  // Convert backend quota-range bounds into integer USD bounds for the input.
  const stakeBounds = useMemo(() => {
    const minQuota = status?.stake_min_quota ?? 0
    const maxQuota = status?.stake_max_quota ?? 0
    const unit = quotaPerUnit > 0 ? quotaPerUnit : 1
    const minUsd = Math.max(1, Math.ceil(minQuota / unit))
    const maxUsd = Math.max(minUsd, Math.floor(maxQuota / unit))
    return { minUsd, maxUsd }
  }, [status?.stake_min_quota, status?.stake_max_quota, quotaPerUnit])

  const dailyBuyCount = Number(status?.daily_buy_count ?? 0)
  const dailyBuyLimit = Number(status?.daily_buy_limit ?? 0)
  const limitReached = dailyBuyLimit > 0 && dailyBuyCount >= dailyBuyLimit
  const lotteryEligible = status?.eligible !== false
  const stakeValid =
    Number.isFinite(stakeUsd) &&
    stakeUsd >= stakeBounds.minUsd &&
    stakeUsd <= stakeBounds.maxUsd

  useEffect(() => {
    setStakeUsd((current) => {
      if (!Number.isFinite(current) || current < stakeBounds.minUsd) {
        return stakeBounds.minUsd
      }
      if (current > stakeBounds.maxUsd) {
        return stakeBounds.maxUsd
      }
      return current
    })
  }, [stakeBounds.maxUsd, stakeBounds.minUsd])

  const drawAtLabel = useMemo(() => {
    if (!status?.draw_at) return '-'
    return dayjs(status.draw_at * 1000).format('YYYY-MM-DD HH:mm')
  }, [status?.draw_at])

  const isPreSale = useMemo(() => {
    if (!status?.draw_at) return false
    return status.draw_at * 1000 - Date.now() > DRAW_AHEAD_PRE_SALE_MS
  }, [status?.draw_at])

  const allocatablePool =
    (status?.pool_carry_in_quota ?? 0) +
    (status?.pool_injected_quota ?? 0) +
    (status?.total_stake_quota ?? 0)

  const toggleRed = useCallback((n: number) => {
    setSelectedRed((prev) => {
      if (prev.includes(n)) {
        return prev.filter((x) => x !== n)
      }
      if (prev.length >= LOTTERY_RED_BALLS_REQUIRED) {
        return prev
      }
      return [...prev, n].sort((a, b) => a - b)
    })
  }, [])

  const selectBlue = useCallback((n: number) => {
    setSelectedBlue(n)
  }, [])

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

  const doBuy = useCallback(
    async (token?: string) => {
      if (selectedRed.length !== LOTTERY_RED_BALLS_REQUIRED) {
        toast.error(t('Please select 5 red balls'))
        return
      }
      if (selectedBlue == null) {
        toast.error(t('Please select 1 blue ball'))
        return
      }
      if (
        !Number.isFinite(stakeUsd) ||
        stakeUsd < stakeBounds.minUsd ||
        stakeUsd > stakeBounds.maxUsd
      ) {
        toast.error(
          t('Stake must be between {{min}} and {{max}}', {
            min: stakeBounds.minUsd,
            max: stakeBounds.maxUsd,
          })
        )
        return
      }

      setSubmitting(true)
      try {
        const res = await buyLotteryTicket(
          {
            red_balls: selectedRed,
            blue_ball: selectedBlue,
            stake_usd: stakeUsd,
          },
          token
        )
        if (res.success && res.data) {
          toast.success(
            t('Ticket purchased for {{amount}}', {
              amount: formatQuotaWithCurrency(res.data.stake_quota),
            })
          )
          syncUserQuota(res.data.new_quota)
          setSelectedRed([])
          setSelectedBlue(null)
          setTurnstileModalVisible(false)
          refetch()
        } else {
          if (!token && shouldTriggerTurnstile(res.message)) {
            if (!turnstileSiteKey) {
              toast.error(t('Turnstile is enabled but site key is empty.'))
              return
            }
            setTurnstileModalVisible(true)
            return
          }
          if (token && shouldTriggerTurnstile(res.message)) {
            setTurnstileWidgetKey((v) => v + 1)
          } else if (token) {
            setTurnstileModalVisible(false)
            setTurnstileWidgetKey((v) => v + 1)
          }
          toast.error(res.message || t('Purchase failed'))
        }
      } catch {
        if (token) {
          setTurnstileModalVisible(false)
          setTurnstileWidgetKey((v) => v + 1)
        }
        toast.error(t('Purchase failed'))
      } finally {
        setSubmitting(false)
      }
    },
    [
      refetch,
      selectedBlue,
      selectedRed,
      shouldTriggerTurnstile,
      stakeBounds.maxUsd,
      stakeBounds.minUsd,
      stakeUsd,
      syncUserQuota,
      t,
      turnstileSiteKey,
    ]
  )

  const handleTurnstileVerify = useCallback(
    (token: string) => {
      doBuy(token)
    },
    [doBuy]
  )

  const handleTurnstileExpire = useCallback(() => {
    setTurnstileWidgetKey((v) => v + 1)
  }, [])

  if (!lotteryEnabled) return null

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
            {t('Failed to load lottery status')}
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

  const selectionReady =
    selectedRed.length === LOTTERY_RED_BALLS_REQUIRED &&
    selectedBlue != null &&
    stakeValid &&
    lotteryEligible &&
    !limitReached

  // Resolve the buy-button label without a nested ternary (lint rule).
  function resolveBuyButtonLabel() {
    if (submitting || turnstileModalVisible) return t('Processing...')
    if (!lotteryEligible) return t('Not eligible')
    if (limitReached) return t('Daily limit reached')
    return t('Buy ticket')
  }
  const buyButtonLabel = resolveBuyButtonLabel()

  return (
    <>
      <Dialog
        open={turnstileModalVisible}
        onOpenChange={(open) => {
          setTurnstileModalVisible(open)
          if (!open) {
            setTurnstileWidgetKey((v) => v + 1)
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
            <div className='bg-amber-500/10 text-amber-600 dark:text-amber-400 flex h-10 w-10 shrink-0 items-center justify-center rounded-xl sm:h-11 sm:w-11'>
              <Ticket className='h-4 w-4 sm:h-5 sm:w-5' strokeWidth={2} />
            </div>
            <div className='min-w-0 flex-1'>
              <h3 className='text-base font-semibold tracking-tight sm:text-lg'>
                {t('Daily Lucky Lottery')}
              </h3>
              <p className='text-muted-foreground mt-1 text-xs sm:text-sm'>
                {t(
                  'Pick 5 red balls (1-{{redMax}}) and 1 blue ball (1-{{blueMax}}); drawn daily',
                  { redMax: redBallMax, blueMax: blueBallMax }
                )}
              </p>
            </div>
          </div>
        </div>

        {/* Stats */}
        <div className='grid grid-cols-3 gap-px border-b'>
          <div className='bg-card p-3 text-center sm:p-5'>
            <div className='text-xl font-semibold tracking-tight tabular-nums sm:text-2xl'>
              {dailyBuyCount}
              <span className='text-muted-foreground'>/{dailyBuyLimit || '-'}</span>
            </div>
            <div className='text-muted-foreground mt-0.5 text-[10px] font-medium sm:mt-1 sm:text-xs'>
              {t('Today purchases')}
            </div>
          </div>
          <div className='bg-card p-3 text-center sm:p-5'>
            <div className='text-xs font-semibold tracking-tight tabular-nums sm:text-lg'>
              {drawAtLabel}
            </div>
            <div className='text-muted-foreground mt-0.5 text-[10px] font-medium sm:mt-1 sm:text-xs'>
              {t('Draw time')}
              {isPreSale && (
                <span className='text-amber-600 dark:text-amber-400 ml-1'>
                  ({t('presale')})
                </span>
              )}
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
          {/* Pool summary */}
          <div className='bg-muted/30 rounded-lg border p-3 text-xs sm:text-sm'>
            <div className='space-y-1.5'>
              <div className='flex justify-between'>
                <span className='text-muted-foreground'>
                  {t('Allocatable pool')}
                </span>
                <span className='font-medium tabular-nums'>
                  {formatQuotaWithCurrency(allocatablePool, { digitsLarge: 0 })}
                </span>
              </div>
              <div className='flex justify-between'>
                <span className='text-muted-foreground'>
                  {t('Carried over from last round')}
                </span>
                <span className='tabular-nums'>
                  {formatQuotaWithCurrency(status.pool_carry_in_quota, {
                    digitsLarge: 0,
                  })}
                </span>
              </div>
              <div className='flex justify-between'>
                <span className='text-muted-foreground'>
                  {t('Total stakes this round')}
                </span>
                <span className='tabular-nums'>
                  {formatQuotaWithCurrency(status.total_stake_quota, {
                    digitsLarge: 0,
                  })}
                </span>
              </div>
              <div className='flex justify-between'>
                <span className='text-muted-foreground'>
                  {t('System injected')}
                </span>
                <span className='tabular-nums'>
                  {formatQuotaWithCurrency(status.pool_injected_quota, {
                    digitsLarge: 0,
                  })}
                </span>
              </div>
            </div>
          </div>

          {/* My current tickets */}
          {status.my_tickets.length > 0 && (
            <TicketList
              title={t('Your tickets this round')}
              tickets={status.my_tickets}
              variant='current'
            />
          )}

          {/* Recent drawn rounds */}
          {status.recent_rounds.length > 0 && (
            <RecentRoundsList rounds={status.recent_rounds} />
          )}

          {/* My recent results */}
          {status.my_recent_tickets.length > 0 && (
            <TicketList
              title={t('Your recent results')}
              tickets={status.my_recent_tickets}
              variant='history'
            />
          )}

          {/* Selection */}
          <div className='border-t pt-4'>
            {!lotteryEligible && status.ineligible_reason && (
              <div className='border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200 mb-4 rounded-lg border px-3 py-2 text-xs sm:text-sm'>
                {status.ineligible_reason}
              </div>
            )}
            <div className='text-muted-foreground mb-2 text-xs font-medium'>
              {t('Pick numbers')}
            </div>

            <div className='mb-2 text-xs font-medium'>{t('Red balls')}</div>
            <div className='mb-4 flex flex-wrap gap-1.5 sm:gap-2'>
              {redOptions.map((n) => {
                const active = selectedRed.includes(n)
                const disabled =
                  !active && selectedRed.length >= LOTTERY_RED_BALLS_REQUIRED
                return (
                  <Button
                    key={`r-${n}`}
                    type='button'
                    size='sm'
                    variant={active ? 'default' : 'outline'}
                    aria-label={t('Red ball {{n}}', { n })}
                    onClick={() => toggleRed(n)}
                    disabled={disabled}
                    className={cn(
                      'h-9 w-9 rounded-full p-0 tabular-nums sm:h-10 sm:w-10',
                      active &&
                        'bg-red-500 text-white hover:bg-red-500/90 dark:bg-red-500 dark:hover:bg-red-500/90'
                    )}
                  >
                    {n}
                  </Button>
                )
              })}
            </div>

            <div className='mb-2 text-xs font-medium'>{t('Blue ball')}</div>
            <div className='mb-4 flex flex-wrap gap-1.5 sm:gap-2'>
              {blueOptions.map((n) => {
                const active = selectedBlue === n
                return (
                  <Button
                    key={`b-${n}`}
                    type='button'
                    size='sm'
                    variant={active ? 'default' : 'outline'}
                    aria-label={t('Blue ball {{n}}', { n })}
                    onClick={() => selectBlue(n)}
                    className={cn(
                      'h-9 w-9 rounded-full p-0 tabular-nums sm:h-10 sm:w-10',
                      active &&
                        'bg-blue-500 text-white hover:bg-blue-500/90 dark:bg-blue-500 dark:hover:bg-blue-500/90'
                    )}
                  >
                    {n}
                  </Button>
                )
              })}
            </div>

            <div className='mb-2 text-xs font-medium'>
              {t('Stake amount (USD)')}
            </div>
            <div className='mb-4 flex flex-wrap items-center gap-2'>
              {LOTTERY_QUICK_STAKES.map((s) => {
                const disabled = s < stakeBounds.minUsd || s > stakeBounds.maxUsd
                return (
                  <Button
                    key={`stake-${s}`}
                    type='button'
                    size='sm'
                    variant={stakeUsd === s ? 'default' : 'outline'}
                    onClick={() => setStakeUsd(s)}
                    disabled={disabled}
                    className='tabular-nums'
                  >
                    ${s}
                  </Button>
                )
              })}
              <input
                type='number'
                min={stakeBounds.minUsd}
                max={stakeBounds.maxUsd}
                step={1}
                value={stakeUsd}
                onChange={(e) => {
                  const next = Number(e.target.value)
                  setStakeUsd(Number.isFinite(next) ? Math.floor(next) : 1)
                }}
                className='bg-background border-input focus-visible:ring-ring h-9 w-24 rounded-md border px-2 text-sm tabular-nums outline-none focus-visible:ring-2'
              />
            </div>
            {!stakeValid && (
              <div className='text-destructive mb-4 text-xs'>
                {t('Stake must be between {{min}} and {{max}}', {
                  min: stakeBounds.minUsd,
                  max: stakeBounds.maxUsd,
                })}
              </div>
            )}

            <div className='flex flex-wrap items-center justify-between gap-2'>
              <div className='text-muted-foreground text-xs'>
                {selectionReady
                  ? `${t('Selected')}: ${selectedRed.join(', ')} + ${selectedBlue}`
                  : t('Pick 5 red and 1 blue ball')}
              </div>
              <Button
                onClick={() => doBuy()}
                disabled={submitting || turnstileModalVisible || !selectionReady}
                size='sm'
                className='w-full sm:w-auto'
              >
                {buyButtonLabel}
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

function TicketList({
  title,
  tickets,
  variant,
}: {
  title: string
  tickets: LotteryTicketView[]
  variant: 'current' | 'history'
}) {
  const { t } = useTranslation()
  return (
    <div>
      <div className='text-muted-foreground mb-1.5 text-xs font-medium'>
        {title}
      </div>
      <div className='flex flex-col gap-1.5'>
        {tickets.map((ticket) => {
          const showResult = variant === 'history' || ticket.result !== 'pending'
          const won = ticket.prize_quota > 0
          return (
            <div
              key={ticket.id}
              className='flex flex-wrap items-center gap-1.5 text-xs sm:text-sm'
            >
              {variant === 'history' && (
                <span className='text-muted-foreground mr-1'>
                  #{ticket.round_id}:
                </span>
              )}
              {ticket.red_balls.map((r) => (
                <BallChip key={`r-${r}`} kind='red' value={r} />
              ))}
              <BallChip kind='blue' value={ticket.blue_ball} />
              <span
                className={cn(
                  'text-muted-foreground ml-1',
                  showResult &&
                    won &&
                    'text-emerald-600 dark:text-emerald-400 font-medium'
                )}
              >
                {t('Stake')}{' '}
                {formatQuotaWithCurrency(ticket.stake_quota, { digitsLarge: 0 })}
                {showResult && (
                  <span className='ml-1'>
                    {won ? '+' : ''}
                    {formatQuotaWithCurrency(ticket.prize_quota, {
                      digitsLarge: 0,
                    })}{' '}
                    ({t(tierLabelKey(ticket.result))})
                  </span>
                )}
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function RecentRoundsList({ rounds }: { rounds: LotteryRoundView[] }) {
  const { t } = useTranslation()
  return (
    <div>
      <div className='text-muted-foreground mb-1.5 text-xs font-medium'>
        {t('Recent draws')}
      </div>
      <div className='flex flex-col gap-1.5'>
        {rounds.map((round) => (
          <div
            key={`round-${round.day}-${round.drawn_at}`}
            className='flex flex-wrap items-center gap-1.5 text-xs sm:text-sm'
          >
            <span className='text-muted-foreground mr-1 tabular-nums'>
              {round.day}:
            </span>
            {round.red_balls.map((r) => (
              <BallChip key={`r-${r}`} kind='red' value={r} />
            ))}
            <BallChip kind='blue' value={round.blue_ball} />
            {round.pool_carry_out_quota > 0 && (
              <span className='text-amber-600 dark:text-amber-400 ml-1'>
                {t('Carryover')}{' '}
                {formatQuotaWithCurrency(round.pool_carry_out_quota, {
                  digitsLarge: 0,
                })}
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

function BallChip({
  kind,
  value,
}: {
  kind: 'red' | 'blue'
  value: number
}) {
  return (
    <span
      className={cn(
        'inline-flex h-5 w-5 items-center justify-center rounded-full text-[10px] font-semibold text-white tabular-nums sm:h-6 sm:w-6 sm:text-xs',
        kind === 'red' ? 'bg-red-500' : 'bg-blue-500'
      )}
    >
      {value}
    </span>
  )
}
