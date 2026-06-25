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
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

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
                      <Input type='number' min={1} placeholder={t('3')} {...field} />
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
                      <Input type='number' min={0} max={23} placeholder={t('22')} {...field} />
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
                      <Input type='number' min={0} placeholder={t('500000')} {...field} />
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
                      <Input type='number' min={0} placeholder={t('50000000')} {...field} />
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
                      <Input type='number' min={0} placeholder={t('0')} {...field} />
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
                      <Input type='number' min={0} placeholder={t('0')} {...field} />
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
    </SettingsSection>
  )
}
