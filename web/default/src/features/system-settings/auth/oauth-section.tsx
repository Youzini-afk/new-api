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
import { zodResolver } from '@hookform/resolvers/zod'
import axios from 'axios'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Button } from '@/components/ui/button'
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
import { Progress } from '@/components/ui/progress'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { formatTimestampToDate } from '@/lib/format'

import {
  getLatestDiscordGatePatrolTask,
  startDiscordGatePatrolTask,
} from '../api'
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import {
  SettingsControlGroup,
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { DiscordGatePatrolTask } from '../types'
import { safeNumberFieldProps } from '../utils/numeric-field'

/**
 * react-hook-form 7 treats dotted `name` strings as nested paths. To keep
 * form state, schema validation, and dirty tracking aligned, the
 * `discord.*` and `oidc.*` fields are modeled as nested objects here and
 * flattened back to dotted server keys only when persisting.
 */
const oauthSchema = z.object({
  GitHubOAuthEnabled: z.boolean(),
  GitHubClientId: z.string(),
  GitHubClientSecret: z.string(),
  discord: z.object({
    enabled: z.boolean(),
    client_id: z.string(),
    client_secret: z.string(),
    register_gate_enabled: z.boolean(),
    register_gate: z.string(),
    login_gate_enabled: z.boolean(),
    login_gate_patrol_enabled: z.boolean(),
    login_gate_patrol_interval_minutes: z.coerce.number().int().min(1).max(60),
    login_gate_patrol_target_sweep_hours: z.coerce
      .number()
      .int()
      .min(1)
      .max(168),
    login_gate_patrol_max_batch_size: z.coerce.number().int().min(50).max(5000),
    login_gate_patrol_worker_count: z.coerce.number().int().min(1).max(32),
    login_gate_patrol_max_rps: z.coerce.number().int().min(1).max(50),
    login_gate_patrol_max_retries: z.coerce.number().int().min(0).max(5),
  }),
  oidc: z.object({
    enabled: z.boolean(),
    client_id: z.string(),
    client_secret: z.string(),
    well_known: z.string(),
    authorization_endpoint: z.string(),
    token_endpoint: z.string(),
    user_info_endpoint: z.string(),
  }),
  TelegramOAuthEnabled: z.boolean(),
  TelegramBotToken: z.string(),
  TelegramBotName: z.string(),
  LinuxDOOAuthEnabled: z.boolean(),
  LinuxDOClientId: z.string(),
  LinuxDOClientSecret: z.string(),
  LinuxDOMinimumTrustLevel: z.string(),
  WeChatAuthEnabled: z.boolean(),
  WeChatServerAddress: z.string(),
  WeChatServerToken: z.string(),
  WeChatAccountQRCodeImageURL: z.string(),
})

type OAuthFormInput = z.input<typeof oauthSchema>
type OAuthFormValues = z.output<typeof oauthSchema>

type FlatOAuthDefaults = {
  GitHubOAuthEnabled: boolean
  GitHubClientId: string
  GitHubClientSecret: string
  'discord.enabled': boolean
  'discord.client_id': string
  'discord.client_secret': string
  'discord.register_gate_enabled': boolean
  'discord.register_gate': string
  'discord.login_gate_enabled': boolean
  'discord.login_gate_patrol_enabled': boolean
  'discord.login_gate_patrol_interval_minutes': number
  'discord.login_gate_patrol_target_sweep_hours': number
  'discord.login_gate_patrol_max_batch_size': number
  'discord.login_gate_patrol_worker_count': number
  'discord.login_gate_patrol_max_rps': number
  'discord.login_gate_patrol_max_retries': number
  'oidc.enabled': boolean
  'oidc.client_id': string
  'oidc.client_secret': string
  'oidc.well_known': string
  'oidc.authorization_endpoint': string
  'oidc.token_endpoint': string
  'oidc.user_info_endpoint': string
  TelegramOAuthEnabled: boolean
  TelegramBotToken: string
  TelegramBotName: string
  LinuxDOOAuthEnabled: boolean
  LinuxDOClientId: string
  LinuxDOClientSecret: string
  LinuxDOMinimumTrustLevel: string
  WeChatAuthEnabled: boolean
  WeChatServerAddress: string
  WeChatServerToken: string
  WeChatAccountQRCodeImageURL: string
}

const oauthTabContentClassName =
  'grid min-w-0 gap-x-5 gap-y-6 lg:grid-cols-2 [&>[data-slot=form-item]]:min-w-0 lg:[&>[data-slot=form-item]:has([data-slot=switch])]:col-span-2'

const buildFormDefaults = (defaults: FlatOAuthDefaults): OAuthFormInput => ({
  GitHubOAuthEnabled: defaults.GitHubOAuthEnabled,
  GitHubClientId: defaults.GitHubClientId ?? '',
  GitHubClientSecret: defaults.GitHubClientSecret ?? '',
  discord: {
    enabled: defaults['discord.enabled'],
    client_id: defaults['discord.client_id'] ?? '',
    client_secret: defaults['discord.client_secret'] ?? '',
    register_gate_enabled: defaults['discord.register_gate_enabled'],
    register_gate: defaults['discord.register_gate'] ?? '',
    login_gate_enabled: defaults['discord.login_gate_enabled'],
    login_gate_patrol_enabled: defaults['discord.login_gate_patrol_enabled'],
    login_gate_patrol_interval_minutes:
      defaults['discord.login_gate_patrol_interval_minutes'],
    login_gate_patrol_target_sweep_hours:
      defaults['discord.login_gate_patrol_target_sweep_hours'],
    login_gate_patrol_max_batch_size:
      defaults['discord.login_gate_patrol_max_batch_size'],
    login_gate_patrol_worker_count:
      defaults['discord.login_gate_patrol_worker_count'],
    login_gate_patrol_max_rps: defaults['discord.login_gate_patrol_max_rps'],
    login_gate_patrol_max_retries:
      defaults['discord.login_gate_patrol_max_retries'],
  },
  oidc: {
    enabled: defaults['oidc.enabled'],
    client_id: defaults['oidc.client_id'] ?? '',
    client_secret: defaults['oidc.client_secret'] ?? '',
    well_known: defaults['oidc.well_known'] ?? '',
    authorization_endpoint: defaults['oidc.authorization_endpoint'] ?? '',
    token_endpoint: defaults['oidc.token_endpoint'] ?? '',
    user_info_endpoint: defaults['oidc.user_info_endpoint'] ?? '',
  },
  TelegramOAuthEnabled: defaults.TelegramOAuthEnabled,
  TelegramBotToken: defaults.TelegramBotToken ?? '',
  TelegramBotName: defaults.TelegramBotName ?? '',
  LinuxDOOAuthEnabled: defaults.LinuxDOOAuthEnabled,
  LinuxDOClientId: defaults.LinuxDOClientId ?? '',
  LinuxDOClientSecret: defaults.LinuxDOClientSecret ?? '',
  LinuxDOMinimumTrustLevel: defaults.LinuxDOMinimumTrustLevel ?? '',
  WeChatAuthEnabled: defaults.WeChatAuthEnabled,
  WeChatServerAddress: defaults.WeChatServerAddress ?? '',
  WeChatServerToken: defaults.WeChatServerToken ?? '',
  WeChatAccountQRCodeImageURL: defaults.WeChatAccountQRCodeImageURL ?? '',
})

const normalizeFormValues = (values: OAuthFormValues): FlatOAuthDefaults => ({
  GitHubOAuthEnabled: values.GitHubOAuthEnabled,
  GitHubClientId: values.GitHubClientId,
  GitHubClientSecret: values.GitHubClientSecret,
  'discord.enabled': values.discord.enabled,
  'discord.client_id': values.discord.client_id,
  'discord.client_secret': values.discord.client_secret,
  'discord.register_gate_enabled': values.discord.register_gate_enabled,
  'discord.register_gate': values.discord.register_gate,
  'discord.login_gate_enabled': values.discord.login_gate_enabled,
  'discord.login_gate_patrol_enabled': values.discord.login_gate_patrol_enabled,
  'discord.login_gate_patrol_interval_minutes':
    values.discord.login_gate_patrol_interval_minutes,
  'discord.login_gate_patrol_target_sweep_hours':
    values.discord.login_gate_patrol_target_sweep_hours,
  'discord.login_gate_patrol_max_batch_size':
    values.discord.login_gate_patrol_max_batch_size,
  'discord.login_gate_patrol_worker_count':
    values.discord.login_gate_patrol_worker_count,
  'discord.login_gate_patrol_max_rps': values.discord.login_gate_patrol_max_rps,
  'discord.login_gate_patrol_max_retries':
    values.discord.login_gate_patrol_max_retries,
  'oidc.enabled': values.oidc.enabled,
  'oidc.client_id': values.oidc.client_id,
  'oidc.client_secret': values.oidc.client_secret,
  'oidc.well_known': values.oidc.well_known,
  'oidc.authorization_endpoint': values.oidc.authorization_endpoint,
  'oidc.token_endpoint': values.oidc.token_endpoint,
  'oidc.user_info_endpoint': values.oidc.user_info_endpoint,
  TelegramOAuthEnabled: values.TelegramOAuthEnabled,
  TelegramBotToken: values.TelegramBotToken,
  TelegramBotName: values.TelegramBotName,
  LinuxDOOAuthEnabled: values.LinuxDOOAuthEnabled,
  LinuxDOClientId: values.LinuxDOClientId,
  LinuxDOClientSecret: values.LinuxDOClientSecret,
  LinuxDOMinimumTrustLevel: values.LinuxDOMinimumTrustLevel,
  WeChatAuthEnabled: values.WeChatAuthEnabled,
  WeChatServerAddress: values.WeChatServerAddress,
  WeChatServerToken: values.WeChatServerToken,
  WeChatAccountQRCodeImageURL: values.WeChatAccountQRCodeImageURL,
})

type OAuthSectionProps = {
  defaultValues: FlatOAuthDefaults
}

export function OAuthSection(props: OAuthSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [activeTab, setActiveTab] = useState('github')

  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<OAuthFormInput, unknown, OAuthFormValues>({
    resolver: zodResolver(oauthSchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<FlatOAuthDefaults>(props.defaultValues)
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(props.defaultValues)
  )

  useEffect(() => {
    const serialized = JSON.stringify(props.defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = props.defaultValues
    baselineSerializedRef.current = serialized
    form.reset(buildFormDefaults(props.defaultValues))
  }, [props.defaultValues, form])

  const onSubmit = async (values: OAuthFormValues) => {
    let finalValues = values

    if (values.oidc.well_known && values.oidc.well_known.trim() !== '') {
      const wellKnown = values.oidc.well_known.trim()
      if (
        !wellKnown.startsWith('http://') &&
        !wellKnown.startsWith('https://')
      ) {
        toast.error(t('Well-Known URL must start with http:// or https://'))
        return
      }

      try {
        const res = await axios.create().get(wellKnown)
        const authEndpoint = res.data['authorization_endpoint'] || ''
        const tokenEndpoint = res.data['token_endpoint'] || ''
        const userInfoEndpoint = res.data['userinfo_endpoint'] || ''

        finalValues = {
          ...values,
          oidc: {
            ...values.oidc,
            authorization_endpoint: authEndpoint,
            token_endpoint: tokenEndpoint,
            user_info_endpoint: userInfoEndpoint,
          },
        }

        form.setValue('oidc.authorization_endpoint', authEndpoint)
        form.setValue('oidc.token_endpoint', tokenEndpoint)
        form.setValue('oidc.user_info_endpoint', userInfoEndpoint)

        toast.success(t('OIDC configuration fetched successfully'))
      } catch (err) {
        // eslint-disable-next-line no-console
        console.error(err)
        toast.error(
          t(
            'Failed to fetch OIDC configuration. Please check the URL and network status'
          )
        )
        return
      }
    }

    const normalized = normalizeFormValues(finalValues)
    const changedKeys = (
      Object.keys(normalized) as Array<keyof FlatOAuthDefaults>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      await updateOption.mutateAsync({
        key,
        value: normalized[key],
      })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
    form.reset(buildFormDefaults(normalized))
  }

  const handleReset = () => {
    form.reset(buildFormDefaults(baselineRef.current))
    toast.success(t('Form reset to saved values'))
  }

  return (
    <>
      <FormNavigationGuard when={form.formState.isDirty} />

      <SettingsSection title={t('OAuth Integrations')}>
        <Form {...form}>
          <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
            <SettingsPageFormActions
              onSave={form.handleSubmit(onSubmit)}
              onReset={handleReset}
              isSaving={updateOption.isPending}
              isResetDisabled={!form.formState.isDirty}
            />
            <FormDirtyIndicator isDirty={form.formState.isDirty} />

            <Tabs value={activeTab} onValueChange={setActiveTab}>
              <TabsList className='grid w-full grid-cols-6'>
                <TabsTrigger value='github'>{t('GitHub')}</TabsTrigger>
                <TabsTrigger value='discord'>{t('Discord')}</TabsTrigger>
                <TabsTrigger value='oidc'>{t('OIDC')}</TabsTrigger>
                <TabsTrigger value='telegram'>{t('Telegram')}</TabsTrigger>
                <TabsTrigger value='linuxdo'>{t('LinuxDO')}</TabsTrigger>
                <TabsTrigger value='wechat'>{t('WeChat')}</TabsTrigger>
              </TabsList>

              <TabsContent value='github' className={oauthTabContentClassName}>
                <FormField
                  control={form.control}
                  name='GitHubOAuthEnabled'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>{t('Enable GitHub OAuth')}</FormLabel>
                        <FormDescription>
                          {t('Allow users to sign in with GitHub')}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='GitHubClientId'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Client ID')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('Your GitHub OAuth Client ID')}
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='GitHubClientSecret'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Client Secret')}</FormLabel>
                      <FormControl>
                        <Input
                          type='password'
                          placeholder={t('Your GitHub OAuth Client Secret')}
                          autoComplete='new-password'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </TabsContent>

              <TabsContent value='discord' className={oauthTabContentClassName}>
                <FormField
                  control={form.control}
                  name='discord.enabled'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>{t('Enable Discord OAuth')}</FormLabel>
                        <FormDescription>
                          {t('Allow users to sign in with Discord')}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='discord.client_id'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Client ID')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('Your Discord OAuth Client ID')}
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='discord.client_secret'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Client Secret')}</FormLabel>
                      <FormControl>
                        <Input
                          type='password'
                          placeholder={t('Your Discord OAuth Client Secret')}
                          autoComplete='new-password'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='discord.register_gate_enabled'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>{t('Register Gate Enabled')}</FormLabel>
                        <FormDescription>
                          {t(
                            'Require new Discord signups and bindings to pass Discord Gate'
                          )}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='discord.register_gate'
                  render={({ field }) => (
                    <FormItem className='lg:col-span-2'>
                      <FormLabel>{t('Register Gate Config')}</FormLabel>
                      <FormControl>
                        <Textarea
                          placeholder={t(
                            '{"groups":[{"rules":[{"guild_id":"...","role_ids":["..."],"role_match":"any","min_join_hours":0}]}]}'
                          )}
                          className='min-h-28 font-mono text-xs'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Nested config supports groups, ban_groups, role_match, min_join_hours'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='discord.login_gate_enabled'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>{t('Login Gate Enabled')}</FormLabel>
                        <FormDescription>
                          {t(
                            'Require existing Discord users to pass Discord Gate during login'
                          )}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />

                <div className='text-muted-foreground space-y-2 rounded-lg border p-3 text-sm lg:col-span-2'>
                  <p className='text-foreground font-medium'>
                    {t('Discord Gate runtime notes')}
                  </p>
                  <p>
                    {t(
                      'Login Gate uses the same Discord Gate config as Register Gate. OAuth login only passes on a confirmed Discord Gate match; temporary Discord API errors require retrying later.'
                    )}
                  </p>
                  <p>
                    {t(
                      'Invite restriction has no separate backend option in this version.'
                    )}
                  </p>
                  <p>
                    {t(
                      'The server Crypto Secret must stay stable because it encrypts stored Discord refresh tokens.'
                    )}
                  </p>
                </div>

                <SettingsControlGroup className='space-y-4'>
                  <div className='space-y-1'>
                    <h4 className='text-sm font-medium'>
                      {t('Discord gate patrol')}
                    </h4>
                    <p className='text-muted-foreground text-sm'>
                      {t(
                        'Periodically rechecks existing Discord users against the configured gate: banned guilds and required guild/role conditions. Users missing the guilds scope only need to reauthorize — their API tokens are not disabled.'
                      )}
                    </p>
                  </div>

                  <FormField
                    control={form.control}
                    name='discord.login_gate_patrol_enabled'
                    render={({ field }) => (
                      <SettingsSwitchItem>
                        <SettingsSwitchContent>
                          <FormLabel>{t('Enable scheduled patrol')}</FormLabel>
                          <FormDescription>
                            {t(
                              'Runs a background batch on the configured interval. Turn off to keep only manual runs.'
                            )}
                          </FormDescription>
                        </SettingsSwitchContent>
                        <FormControl>
                          <Switch
                            checked={field.value}
                            onCheckedChange={field.onChange}
                          />
                        </FormControl>
                      </SettingsSwitchItem>
                    )}
                  />

                  <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3'>
                    <FormField
                      control={form.control}
                      name='discord.login_gate_patrol_interval_minutes'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Interval (minutes)')}</FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              min={1}
                              max={60}
                              step={1}
                              {...safeNumberFieldProps(field)}
                            />
                          </FormControl>
                          <FormDescription>
                            {t('How often the scheduled patrol runs. (1–60)')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name='discord.login_gate_patrol_target_sweep_hours'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>
                            {t('Target sweep window (hours)')}
                          </FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              min={1}
                              max={168}
                              step={1}
                              {...safeNumberFieldProps(field)}
                            />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'Batch size is sized to cover all eligible users within this window. (1–168)'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name='discord.login_gate_patrol_max_batch_size'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Max batch size')}</FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              min={50}
                              max={5000}
                              step={1}
                              {...safeNumberFieldProps(field)}
                            />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'Upper bound for users checked per run. (50–5000)'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name='discord.login_gate_patrol_worker_count'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Workers')}</FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              min={1}
                              max={32}
                              step={1}
                              {...safeNumberFieldProps(field)}
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Concurrent Discord API workers. (1–32)')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name='discord.login_gate_patrol_max_rps'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Max RPS')}</FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              min={1}
                              max={50}
                              step={1}
                              {...safeNumberFieldProps(field)}
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Discord API rate limit across workers. (1–50)')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name='discord.login_gate_patrol_max_retries'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Max retries')}</FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              min={0}
                              max={5}
                              step={1}
                              {...safeNumberFieldProps(field)}
                            />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'Retries per user on transient Discord errors. (0–5)'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>

                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Patrol needs the Discord `guilds` scope to read guild membership. Existing users who never granted it must reauthorize before they can be fully checked.'
                    )}
                  </p>

                  <DiscordGatePatrolControls />
                </SettingsControlGroup>
              </TabsContent>

              <TabsContent value='oidc' className={oauthTabContentClassName}>
                <FormField
                  control={form.control}
                  name='oidc.enabled'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>{t('Enable OIDC')}</FormLabel>
                        <FormDescription>
                          {t('Allow users to sign in with OpenID Connect')}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='oidc.client_id'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Client ID')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('OIDC Client ID')}
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='oidc.client_secret'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Client Secret')}</FormLabel>
                      <FormControl>
                        <Input
                          type='password'
                          placeholder={t('OIDC Client Secret')}
                          autoComplete='new-password'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='oidc.well_known'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Well-Known URL')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t(
                            'https://provider.com/.well-known/openid-configuration'
                          )}
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Auto-discovers endpoints from the provider')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='oidc.authorization_endpoint'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Authorization Endpoint (Optional)')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('Override auto-discovered endpoint')}
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='oidc.token_endpoint'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Token Endpoint (Optional)')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('Override auto-discovered endpoint')}
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='oidc.user_info_endpoint'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('User Info Endpoint (Optional)')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('Override auto-discovered endpoint')}
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </TabsContent>

              <TabsContent
                value='telegram'
                className={oauthTabContentClassName}
              >
                <FormField
                  control={form.control}
                  name='TelegramOAuthEnabled'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>{t('Enable Telegram OAuth')}</FormLabel>
                        <FormDescription>
                          {t('Allow users to sign in with Telegram')}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='TelegramBotToken'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Bot Token')}</FormLabel>
                      <FormControl>
                        <Input
                          type='password'
                          placeholder={t('Your Telegram Bot Token')}
                          autoComplete='new-password'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='TelegramBotName'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Bot Name')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('Your Bot Name')}
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </TabsContent>

              <TabsContent value='linuxdo' className={oauthTabContentClassName}>
                <FormField
                  control={form.control}
                  name='LinuxDOOAuthEnabled'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>{t('Enable LinuxDO OAuth')}</FormLabel>
                        <FormDescription>
                          {t('Allow users to sign in with LinuxDO')}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='LinuxDOClientId'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Client ID')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('LinuxDO Client ID')}
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='LinuxDOClientSecret'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Client Secret')}</FormLabel>
                      <FormControl>
                        <Input
                          type='password'
                          placeholder={t('LinuxDO Client Secret')}
                          autoComplete='new-password'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='LinuxDOMinimumTrustLevel'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Minimum Trust Level')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder='0'
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Minimum LinuxDO trust level required')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </TabsContent>

              <TabsContent value='wechat' className={oauthTabContentClassName}>
                <FormField
                  control={form.control}
                  name='WeChatAuthEnabled'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>{t('Enable WeChat Auth')}</FormLabel>
                        <FormDescription>
                          {t('Allow users to sign in with WeChat')}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='WeChatServerAddress'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Server Address')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('https://wechat-server.example.com')}
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='WeChatServerToken'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Server Token')}</FormLabel>
                      <FormControl>
                        <Input
                          type='password'
                          placeholder={t('Server Token')}
                          autoComplete='new-password'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='WeChatAccountQRCodeImageURL'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('QR Code Image URL')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('https://example.com/qr-code.png')}
                          autoComplete='off'
                          value={field.value ?? ''}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          name={field.name}
                          onBlur={field.onBlur}
                          ref={field.ref}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </TabsContent>
            </Tabs>
          </SettingsForm>
        </Form>
      </SettingsSection>
    </>
  )
}

const DISCORD_PATROL_OUTCOME_LABELS: Record<string, string> = {
  pass: 'Passed',
  ban_matched: 'Banned guild match',
  allow_failed: 'Gate failed',
  reauth_required: 'Reauthorization required',
  transient: 'Transient error',
  skipped: 'Skipped',
}

const DISCORD_PATROL_POLL_INTERVAL_MS = 3000

function isPatrolActive(task: DiscordGatePatrolTask | null) {
  return task?.status === 'pending' || task?.status === 'running'
}

function patrolStatusLabelKey(status: string): string {
  if (status === 'pending') return 'Pending'
  if (status === 'running') return 'Running'
  if (status === 'succeeded') return 'Completed'
  return 'Failed'
}

function patrolModeLabelKey(mode?: string): string {
  if (mode === 'manual_batch') return 'Manual patrol'
  if (mode === 'scheduled') return 'Scheduled patrol'
  return 'Unknown mode'
}

function patrolStatusTone(status?: string): string {
  if (status === 'pending' || status === 'running') {
    return 'border-blue-500/30 bg-blue-500/10 text-blue-500'
  }
  if (status === 'succeeded') {
    return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-500'
  }
  if (status === 'failed') {
    return 'border-destructive/30 bg-destructive/10 text-destructive'
  }
  return 'border-muted-foreground/30 bg-muted text-muted-foreground'
}

function DiscordGatePatrolControls() {
  const { t } = useTranslation()
  const [task, setTask] = useState<DiscordGatePatrolTask | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [isStarting, setIsStarting] = useState(false)
  const [batchSizeInput, setBatchSizeInput] = useState('')

  const fetchTask = useCallback(async () => {
    setIsLoading(true)
    try {
      const res = await getLatestDiscordGatePatrolTask()
      if (res.success && res.data) {
        setTask(res.data)
      } else {
        setTask(null)
      }
    } catch {
      /* ignore — refetch button is available */
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchTask()
  }, [fetchTask])

  const taskStatus = task?.status
  useEffect(() => {
    if (taskStatus !== 'pending' && taskStatus !== 'running') return
    const interval = window.setInterval(() => {
      fetchTask()
    }, DISCORD_PATROL_POLL_INTERVAL_MS)
    return () => {
      window.clearInterval(interval)
    }
  }, [taskStatus, fetchTask])

  const handleRun = async () => {
    setIsStarting(true)
    try {
      const trimmed = batchSizeInput.trim()
      const parsed = trimmed === '' ? Number.NaN : Number(trimmed)
      const request =
        Number.isFinite(parsed) && parsed > 0
          ? { batch_size: parsed }
          : undefined
      const res = await startDiscordGatePatrolTask(request)
      if (!res.success || !res.data) {
        throw new Error(res.message || t('Failed to start patrol batch'))
      }
      setTask(res.data)
      setBatchSizeInput('')
      toast.success(t('Patrol batch started.'))
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : t('Failed to start patrol batch')
      toast.error(message)
    } finally {
      setIsStarting(false)
    }
  }

  const active = isPatrolActive(task)
  const progress = Math.min(100, Math.max(0, task?.state?.progress ?? 0))
  const processed = task?.state?.processed ?? 0
  const total = task?.state?.total ?? 0
  const counts = task?.result?.counts ?? {}
  const hasCounts = Object.keys(counts).length > 0
  const mode = task?.payload?.mode

  return (
    <div className='space-y-3 border-t pt-3'>
      <div className='flex flex-wrap items-end gap-3'>
        <div className='grid gap-1.5'>
          <FormLabel className='text-xs'>
            {t('Manual batch size (optional)')}
          </FormLabel>
          <Input
            type='number'
            min={50}
            max={5000}
            step={1}
            placeholder={t('Uses saved max batch size if empty')}
            value={batchSizeInput}
            onChange={(event) => setBatchSizeInput(event.target.value)}
            className='w-[200px]'
          />
        </div>
        <Button
          type='button'
          onClick={handleRun}
          disabled={isStarting || active}
        >
          {isStarting || active ? t('Running...') : t('Run patrol batch now')}
        </Button>
        <Button
          type='button'
          variant='outline'
          onClick={fetchTask}
          disabled={isLoading}
        >
          {isLoading ? t('Refreshing...') : t('Refresh status')}
        </Button>
      </div>

      <div className='rounded-md border p-3'>
        <div className='mb-2 flex items-center justify-between gap-3 text-sm'>
          <span className='font-medium'>{t('Current patrol status')}</span>
          {task ? (
            <span
              className={`rounded-full border px-2 py-0.5 text-xs font-medium ${patrolStatusTone(task.status)}`}
            >
              {t(patrolStatusLabelKey(task.status))}
            </span>
          ) : (
            <span className='text-muted-foreground text-xs'>
              {t('No patrol has run yet')}
            </span>
          )}
        </div>

        {!task ? (
          <div className='text-muted-foreground text-xs'>
            {t('Click Run patrol batch now to start a manual check.')}
          </div>
        ) : (
          <>
            <div className='text-muted-foreground mb-2 grid gap-1 text-xs sm:grid-cols-3'>
              <div>
                <span className='text-foreground font-medium'>
                  {t('Mode')}:{' '}
                </span>
                {t(patrolModeLabelKey(mode))}
              </div>
              <div>
                <span className='text-foreground font-medium'>
                  {t('Updated')}:{' '}
                </span>
                {formatTimestampToDate(task.updated_at)}
              </div>
              <div className='truncate'>
                <span className='text-foreground font-medium'>
                  {t('Task ID')}:{' '}
                </span>
                <span title={task.task_id}>{task.task_id}</span>
              </div>
            </div>

            {(active || progress > 0 || total > 0) && (
              <>
                <Progress value={progress} />
                <div className='text-muted-foreground mt-2 text-xs'>
                  {t('{{processed}} of {{total}} users checked.', {
                    processed,
                    total,
                  })}
                </div>
              </>
            )}
            {task.status === 'failed' && task.error && (
              <div className='text-destructive mt-2 text-xs'>{task.error}</div>
            )}
            {hasCounts && (
              <div className='mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs'>
                {Object.entries(counts).map(([key, count]) => (
                  <span key={key} className='text-muted-foreground'>
                    <span className='text-foreground font-medium'>
                      {t(DISCORD_PATROL_OUTCOME_LABELS[key] ?? key)}:
                    </span>{' '}
                    {count}
                  </span>
                ))}
              </div>
            )}
            {task.result?.circuit_breaker && (
              <div className='text-destructive mt-2 text-xs'>
                {t(
                  'Circuit breaker tripped: too many transient errors. Try again later.'
                )}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
