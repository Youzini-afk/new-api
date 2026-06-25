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
import { z } from 'zod'
import { useForm, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { ROULETTE_DEFAULT_WHEEL_JSON, ROULETTE_MAX_RTP_BPS } from '@/features/games/constants'

// ============================================================================
// Lottery section
// ============================================================================

const schema = z.object({
  lotteryEnabled: z.boolean(),
  dailyBuyLimit: z.coerce.number().int().min(1),
  minStakeQuota: z.coerce.number().int().min(0),
  maxStakeQuota: z.coerce.number().int().min(0),
  systemInjectedQuota: z.coerce.number().int().min(0),
  maxUserQuota: z.coerce.number().int().min(0),
  drawHour: z.coerce.number().int().min(0).max(23),
})

type Values = z.infer<typeof schema>

export function GameSettingsSection({
  defaultValues,
}: {
  defaultValues: {
    lotteryEnabled: boolean
    dailyBuyLimit: number
    minStakeQuota: number
    maxStakeQuota: number
    systemInjectedQuota: number
    maxUserQuota: number
    drawHour: number
    rouletteEnabled: boolean
    rouletteDailySpinLimit: number
    rouletteMinStakeQuota: number
    rouletteMaxStakeQuota: number
    rouletteMaxDailyStakeQuota: number
    rouletteMaxUserQuota: number
    rouletteRtpBps: number
    rouletteWheel: string
  }
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: {
      lotteryEnabled: defaultValues.lotteryEnabled,
      dailyBuyLimit: defaultValues.dailyBuyLimit,
      minStakeQuota: defaultValues.minStakeQuota,
      maxStakeQuota: defaultValues.maxStakeQuota,
      systemInjectedQuota: defaultValues.systemInjectedQuota,
      maxUserQuota: defaultValues.maxUserQuota,
      drawHour: defaultValues.drawHour,
    },
  })

  const { isDirty, isSubmitting } = form.formState
  const lotteryEnabled = form.watch('lotteryEnabled')

  async function onSubmit(values: Values) {
    const updates: Array<{ key: string; value: string }> = []

    if (values.lotteryEnabled !== defaultValues.lotteryEnabled) {
      updates.push({
        key: 'game_setting.lottery_enabled',
        value: String(values.lotteryEnabled),
      })
    }
    if (values.dailyBuyLimit !== defaultValues.dailyBuyLimit) {
      updates.push({
        key: 'game_setting.lottery_daily_buy_limit',
        value: String(values.dailyBuyLimit),
      })
    }
    if (values.minStakeQuota !== defaultValues.minStakeQuota) {
      updates.push({
        key: 'game_setting.lottery_min_stake_quota',
        value: String(values.minStakeQuota),
      })
    }
    if (values.maxStakeQuota !== defaultValues.maxStakeQuota) {
      updates.push({
        key: 'game_setting.lottery_max_stake_quota',
        value: String(values.maxStakeQuota),
      })
    }
    if (values.systemInjectedQuota !== defaultValues.systemInjectedQuota) {
      updates.push({
        key: 'game_setting.lottery_system_injected_quota',
        value: String(values.systemInjectedQuota),
      })
    }
    if (values.maxUserQuota !== defaultValues.maxUserQuota) {
      updates.push({
        key: 'game_setting.lottery_max_user_quota',
        value: String(values.maxUserQuota),
      })
    }
    if (values.drawHour !== defaultValues.drawHour) {
      updates.push({
        key: 'game_setting.lottery_draw_hour',
        value: String(values.drawHour),
      })
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }

    form.reset(values)
  }

  return (
    <SettingsSection title={t('Game Settings')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save game settings'
          />
          <FormField
            control={form.control}
            name='lotteryEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable daily lottery')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Allow users to buy lottery tickets for a daily draw. Disabled by default.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          {lotteryEnabled && (
            <div className='grid gap-6 sm:grid-cols-2'>
              <FormField
                control={form.control}
                name='dailyBuyLimit'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Daily buy limit per user')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={1} placeholder='3' {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Maximum tickets a single user can buy per round (including duplicate-number checks).'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='drawHour'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Daily draw hour (0-23)')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} max={23} placeholder='22' {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Hour of the day (local time) when each round is drawn. Invalid values fall back to 22.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='minStakeQuota'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Minimum stake (quota)')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} placeholder='500000' {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Minimum quota a user must stake on a single ticket. Must be at least 1.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='maxStakeQuota'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Maximum stake (quota)')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} placeholder='50000000' {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Maximum quota a user can stake on a single ticket. Must be at least the minimum.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='systemInjectedQuota'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('System injected quota per round')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} placeholder='0' {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Quota the system adds to every round prize pool. 0 means no system subsidy (safer default).'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='maxUserQuota'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Maximum user quota for lottery')}</FormLabel>
                    <FormControl>
                      <Input type='number' min={0} placeholder='0' {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Users with quota at or above this limit cannot buy tickets (prevents whale abuse). 0 means no limit.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          )}
        </SettingsForm>
      </Form>

      <RouletteSettingsSection defaultValues={defaultValues} />
    </SettingsSection>
  )
}

// ============================================================================
// Roulette section (rendered as a subsection under Game Settings)
// ============================================================================

const ROULETTE_MAX_COMPONENT_VALUE = 2 ** 30
const ROULETTE_MAX_TOTAL_WEIGHT = 2 ** 31

// Validate the wheel JSON shape: when non-empty, must parse to an array of
// outcomes with required numeric fields. Empty string is allowed (the
// backend falls back to its built-in default wheel).
const createWheelJsonSchema = (t: (key: string, opts?: Record<string, unknown>) => string) =>
  z
    .string()
    .superRefine((value, ctx) => {
      if (!value || value.trim() === '') return
      let parsed: unknown
      try {
        parsed = JSON.parse(value)
      } catch {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('Wheel config must be valid JSON or empty'),
          path: [],
        })
        return
      }
      if (!Array.isArray(parsed)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('Wheel config must be a JSON array of outcomes'),
          path: [],
        })
        return
      }
      if (parsed.length === 0) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('Wheel config must contain at least one outcome'),
          path: [],
        })
        return
      }
      const seenKeys = new Set<string>()
      for (let i = 0; i < parsed.length; i += 1) {
        const item = parsed[i] as Record<string, unknown> | null
        if (!item || typeof item !== 'object') {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: t('Wheel outcome at index {{i}} is not an object', { i }),
            path: [i],
          })
          continue
        }
        if (typeof item.key !== 'string' || item.key.trim().length === 0) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: t('Wheel outcome at index {{i}} is missing a key', { i }),
            path: [i],
          })
        } else if (seenKeys.has(item.key)) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: t('Wheel outcome key must be unique: {{key}}', {
              key: item.key,
            }),
            path: [i],
          })
        } else {
          seenKeys.add(item.key)
        }
        if (
          typeof item.multiplier_bps !== 'number' ||
          !Number.isFinite(item.multiplier_bps) ||
          !Number.isInteger(item.multiplier_bps) ||
          item.multiplier_bps < 0 ||
          item.multiplier_bps > ROULETTE_MAX_COMPONENT_VALUE
        ) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: t(
              'Wheel outcome at index {{i}} has an invalid multiplier_bps',
              { i }
            ),
            path: [i],
          })
        }
        if (
          typeof item.weight !== 'number' ||
          !Number.isFinite(item.weight) ||
          !Number.isInteger(item.weight) ||
          item.weight <= 0 ||
          item.weight > ROULETTE_MAX_COMPONENT_VALUE
        ) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: t('Wheel outcome at index {{i}} has an invalid weight', {
              i,
            }),
            path: [i],
          })
        }
      }
    })

const createRouletteSchema = (
  t: (key: string, opts?: Record<string, unknown>) => string
) =>
  z
    .object({
      rouletteEnabled: z.boolean(),
      rouletteDailySpinLimit: z.coerce.number().int().min(0),
      rouletteMinStakeQuota: z.coerce.number().int().min(1),
      rouletteMaxStakeQuota: z.coerce.number().int().min(1),
      rouletteMaxDailyStakeQuota: z.coerce.number().int().min(0),
      rouletteMaxUserQuota: z.coerce.number().int().min(0),
      rouletteRtpBps: z.coerce
        .number()
        .int()
        .min(0)
        .max(ROULETTE_MAX_RTP_BPS),
      rouletteWheel: createWheelJsonSchema(t),
    })
    .superRefine((values, ctx) => {
      // Cross-field: max stake must be at least the min stake. Without this
      // an operator could lock the game to an unsatisfiable range.
      if (
        Number.isFinite(values.rouletteMinStakeQuota) &&
        Number.isFinite(values.rouletteMaxStakeQuota) &&
        values.rouletteMaxStakeQuota < values.rouletteMinStakeQuota
      ) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t(
            'Maximum stake must be at least the minimum stake'
          ),
          path: ['rouletteMaxStakeQuota'],
        })
      }

      const wheelRaw = values.rouletteWheel.trim()
      if (wheelRaw === '') return

      let parsed: unknown
      try {
        parsed = JSON.parse(wheelRaw)
      } catch {
        return
      }
      if (!Array.isArray(parsed)) return

      let totalWeight = 0
      let totalWeightBig = 0n
      let sumProduct = 0n
      for (const entry of parsed) {
        if (!entry || typeof entry !== 'object') return
        const outcome = entry as Record<string, unknown>
        if (
          typeof outcome.weight !== 'number' ||
          typeof outcome.multiplier_bps !== 'number' ||
          !Number.isFinite(outcome.weight) ||
          !Number.isFinite(outcome.multiplier_bps) ||
          !Number.isInteger(outcome.weight) ||
          !Number.isInteger(outcome.multiplier_bps) ||
          outcome.weight <= 0 ||
          outcome.weight > ROULETTE_MAX_COMPONENT_VALUE ||
          outcome.multiplier_bps < 0 ||
          outcome.multiplier_bps > ROULETTE_MAX_COMPONENT_VALUE
        ) {
          return
        }
        totalWeight += outcome.weight
        totalWeightBig += BigInt(outcome.weight)
        sumProduct += BigInt(outcome.weight) * BigInt(outcome.multiplier_bps)
      }

      if (totalWeight > ROULETTE_MAX_TOTAL_WEIGHT) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('Wheel total weight must be at most {{max}}', {
            max: ROULETTE_MAX_TOTAL_WEIGHT,
          }),
          path: ['rouletteWheel'],
        })
      }

      const maxReturn = BigInt(values.rouletteRtpBps) * totalWeightBig
      if (sumProduct > maxReturn) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('Wheel expected RTP must not exceed the configured cap'),
          path: ['rouletteWheel'],
        })
      }
    })

type RouletteValues = z.infer<ReturnType<typeof createRouletteSchema>>

function RouletteSettingsSection({
  defaultValues,
}: {
  defaultValues: {
    rouletteEnabled: boolean
    rouletteDailySpinLimit: number
    rouletteMinStakeQuota: number
    rouletteMaxStakeQuota: number
    rouletteMaxDailyStakeQuota: number
    rouletteMaxUserQuota: number
    rouletteRtpBps: number
    rouletteWheel: string
  }
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const rouletteSchema = createRouletteSchema(t)
  const form = useForm<RouletteValues>({
    resolver: zodResolver(rouletteSchema) as unknown as Resolver<RouletteValues>,
    defaultValues: {
      rouletteEnabled: defaultValues.rouletteEnabled,
      rouletteDailySpinLimit: defaultValues.rouletteDailySpinLimit,
      rouletteMinStakeQuota: defaultValues.rouletteMinStakeQuota,
      rouletteMaxStakeQuota: defaultValues.rouletteMaxStakeQuota,
      rouletteMaxDailyStakeQuota: defaultValues.rouletteMaxDailyStakeQuota,
      rouletteMaxUserQuota: defaultValues.rouletteMaxUserQuota,
      rouletteRtpBps: defaultValues.rouletteRtpBps,
      rouletteWheel:
        defaultValues.rouletteWheel || ROULETTE_DEFAULT_WHEEL_JSON,
    },
  })

  const { isDirty, isSubmitting } = form.formState
  const rouletteEnabled = form.watch('rouletteEnabled')
  const rtpBps = form.watch('rouletteRtpBps')
  const rtpTooHigh =
    Number.isFinite(rtpBps) && rtpBps > ROULETTE_MAX_RTP_BPS

  async function onSubmit(values: RouletteValues) {
    const defaults = form.formState.defaultValues as RouletteValues
    const updates: Array<{ key: string; value: string }> = []

    if (values.rouletteEnabled !== defaults.rouletteEnabled) {
      updates.push({
        key: 'game_setting.roulette_enabled',
        value: String(values.rouletteEnabled),
      })
    }
    if (values.rouletteDailySpinLimit !== defaults.rouletteDailySpinLimit) {
      updates.push({
        key: 'game_setting.roulette_daily_spin_limit',
        value: String(values.rouletteDailySpinLimit),
      })
    }
    if (values.rouletteMinStakeQuota !== defaults.rouletteMinStakeQuota) {
      updates.push({
        key: 'game_setting.roulette_min_stake_quota',
        value: String(values.rouletteMinStakeQuota),
      })
    }
    if (values.rouletteMaxStakeQuota !== defaults.rouletteMaxStakeQuota) {
      updates.push({
        key: 'game_setting.roulette_max_stake_quota',
        value: String(values.rouletteMaxStakeQuota),
      })
    }
    if (
      values.rouletteMaxDailyStakeQuota !==
      defaults.rouletteMaxDailyStakeQuota
    ) {
      updates.push({
        key: 'game_setting.roulette_max_daily_stake_quota',
        value: String(values.rouletteMaxDailyStakeQuota),
      })
    }
    if (values.rouletteMaxUserQuota !== defaults.rouletteMaxUserQuota) {
      updates.push({
        key: 'game_setting.roulette_max_user_quota',
        value: String(values.rouletteMaxUserQuota),
      })
    }
    if (values.rouletteRtpBps !== defaults.rouletteRtpBps) {
      updates.push({
        key: 'game_setting.roulette_rtp_bps',
        value: String(values.rouletteRtpBps),
      })
    }
    if (values.rouletteWheel !== defaults.rouletteWheel) {
      updates.push({
        key: 'game_setting.roulette_wheel',
        value: String(values.rouletteWheel),
      })
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }

    form.reset(values)
  }

  return (
    <div className='border-t pt-6'>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save roulette settings'
          />

          <div className='mb-4'>
            <h4 className='text-sm font-semibold tracking-tight'>
              {t('Roulette')}
            </h4>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'Paid instant-spin game. Stake is deducted before payout; wheel RTP is capped and spins are rate-limited.'
              )}
            </p>
          </div>

          <FormField
            control={form.control}
            name='rouletteEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable roulette')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Allow users to spin the wheel for quota. Disabled by default; the backend also hides the card when off.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          {rouletteEnabled && (
            <div className='space-y-6'>
              <div className='border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200 rounded-lg border px-3 py-2 text-xs'>
                {t(
                  'Stake is deducted before each spin and only the quota-cap-adjusted prize is returned. Keep wheel RTP at or below 95% to avoid long-term operator loss.'
                )}
              </div>

              <div className='grid gap-6 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='rouletteDailySpinLimit'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Daily spin limit per user')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          placeholder='3'
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Max spins a single user can perform per day. 0 fails closed (no spins allowed).'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='rouletteRtpBps'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('House RTP cap (basis points, 0-9500)')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          max={ROULETTE_MAX_RTP_BPS}
                          placeholder='9000'
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Max long-run return-to-player in basis points. 9500 = 95%. Higher values increase operator risk.'
                        )}
                      </FormDescription>
                      {rtpTooHigh && (
                        <FormMessage>
                          {t('RTP must be at most {{max}} bps', {
                            max: ROULETTE_MAX_RTP_BPS,
                          })}
                        </FormMessage>
                      )}
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='rouletteMinStakeQuota'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Minimum stake (quota)')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          placeholder='500000'
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Minimum quota a user can stake on a single spin. Must be at least 1.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='rouletteMaxStakeQuota'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Maximum stake (quota)')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          placeholder='5000000'
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Maximum quota a user can stake on a single spin. Must be at least the minimum.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='rouletteMaxDailyStakeQuota'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Daily stake cap per user (quota)')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          placeholder='0'
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Optional cap on the total quota a single user can stake per day. 0 means no extra cap (the per-spin max still applies).'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='rouletteMaxUserQuota'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Maximum user quota for roulette')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          placeholder='0'
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Users with quota at or above this limit cannot spin (prevents whale abuse). 0 means no limit.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              <FormField
                control={form.control}
                name='rouletteWheel'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Wheel outcomes (advanced JSON)')}
                    </FormLabel>
                    <FormControl>
                      <Textarea
                        rows={6}
                        spellCheck={false}
                        placeholder={ROULETTE_DEFAULT_WHEEL_JSON}
                        className='font-mono text-xs'
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Array of outcomes: [{key, multiplier_bps, weight}]. multiplier_bps uses basis points (10000 = 1x). Empty falls back to the backend default. Invalid JSON will not save.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          )}
        </SettingsForm>
      </Form>
    </div>
  )
}
