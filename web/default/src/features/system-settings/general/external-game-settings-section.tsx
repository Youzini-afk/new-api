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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
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
import { Switch } from '@/components/ui/switch'

import { getExternalGameSettings, updateExternalGameSettings } from '../api'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { useResetForm } from '../hooks/use-reset-form'
import type {
  ExternalGameEnvironmentOverrides,
  ExternalGameSettings,
  UpdateExternalGameSettingsRequest,
} from '../types'

const EMPTY_OVERRIDES: ExternalGameEnvironmentOverrides = {
  enabled: false,
  app_id: false,
  app_secret: false,
  redirect_uri: false,
  code_ttl_seconds: false,
  signature_tolerance_seconds: false,
}

function validRedirectUri(value: string) {
  try {
    const url = new URL(value)
    if (url.protocol === 'https:') return true
    return (
      url.protocol === 'http:' &&
      ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
    )
  } catch {
    return false
  }
}

const createSchema = (t: (key: string) => string, secretConfigured: boolean) =>
  z
    .object({
      enabled: z.boolean(),
      app_id: z.string(),
      app_secret: z.string(),
      redirect_uri: z.string(),
      code_ttl_seconds: z.coerce.number().int().min(30).max(600),
      signature_tolerance_seconds: z.coerce.number().int().min(30).max(600),
    })
    .superRefine((values, ctx) => {
      const appId = values.app_id.trim()
      const secret = values.app_secret.trim()
      const redirectUri = values.redirect_uri.trim()

      if (values.enabled && appId === '') {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t(
            'Application ID is required when the integration is enabled.'
          ),
          path: ['app_id'],
        })
      }
      if (secret !== '' && secret.length < 16) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('Application secret must contain at least 16 characters.'),
          path: ['app_secret'],
        })
      }
      if (values.enabled && !secretConfigured && secret === '') {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t(
            'Application secret is required when the integration is enabled.'
          ),
          path: ['app_secret'],
        })
      }
      if (redirectUri !== '' && !validRedirectUri(redirectUri)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t(
            'The redirect URI must use HTTPS, except localhost development URLs.'
          ),
          path: ['redirect_uri'],
        })
      } else if (values.enabled && redirectUri === '') {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t(
            'Redirect URI is required when the integration is enabled.'
          ),
          path: ['redirect_uri'],
        })
      }
    })

type ExternalGameFormValues = z.infer<ReturnType<typeof createSchema>>

function toFormValues(settings?: ExternalGameSettings): ExternalGameFormValues {
  return {
    enabled: settings?.enabled ?? false,
    app_id: settings?.app_id ?? 'wtfib',
    app_secret: '',
    redirect_uri: settings?.redirect_uri ?? '',
    code_ttl_seconds: settings?.code_ttl_seconds ?? 120,
    signature_tolerance_seconds: settings?.signature_tolerance_seconds ?? 300,
  }
}

function EnvironmentHint({ name }: { name: string }) {
  const { t } = useTranslation()
  return (
    <span className='text-amber-600 dark:text-amber-400'>
      {t('Managed by environment variable {{name}}.', { name })}
    </span>
  )
}

export function ExternalGameSettingsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const settingsQuery = useQuery({
    queryKey: ['system-settings', 'external-game'],
    queryFn: async () => {
      const response = await getExternalGameSettings()
      if (!response.success || !response.data) {
        throw new Error(
          response.message || t('Failed to load external game settings')
        )
      }
      return response.data
    },
  })

  const schema = useMemo(
    () => createSchema(t, settingsQuery.data?.app_secret_configured ?? false),
    [settingsQuery.data?.app_secret_configured, t]
  )
  const form = useForm<ExternalGameFormValues>({
    resolver: zodResolver(
      schema
    ) as unknown as Resolver<ExternalGameFormValues>,
    defaultValues: toFormValues(),
  })
  const formValues = useMemo(
    () => toFormValues(settingsQuery.data),
    [settingsQuery.data]
  )
  useResetForm(form, formValues)

  const mutation = useMutation({
    mutationFn: async (request: UpdateExternalGameSettingsRequest) => {
      const response = await updateExternalGameSettings(request)
      if (!response.success || !response.data) {
        throw new Error(
          response.message || t('Failed to update external game settings')
        )
      }
      return response.data
    },
    onSuccess: (settings) => {
      queryClient.setQueryData(['system-settings', 'external-game'], settings)
      form.reset(toFormValues(settings))
      toast.success(t('External game settings updated successfully'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to update external game settings'))
    },
  })

  const settings = settingsQuery.data
  const overrides = settings?.environment_overrides ?? EMPTY_OVERRIDES
  const isBusy = settingsQuery.isLoading || mutation.isPending
  const { isDirty, isSubmitting } = form.formState

  const onSubmit = (values: ExternalGameFormValues) => {
    const request: UpdateExternalGameSettingsRequest = {}
    if (!overrides.enabled) request.enabled = values.enabled
    if (!overrides.app_id) request.app_id = values.app_id.trim()
    if (!overrides.redirect_uri) {
      request.redirect_uri = values.redirect_uri.trim()
    }
    if (!overrides.code_ttl_seconds) {
      request.code_ttl_seconds = values.code_ttl_seconds
    }
    if (!overrides.signature_tolerance_seconds) {
      request.signature_tolerance_seconds = values.signature_tolerance_seconds
    }
    const secret = values.app_secret.trim()
    if (!overrides.app_secret && secret !== '') {
      request.app_secret = secret
    }
    mutation.mutate(request)
  }

  let secretStatus = t('Not configured')
  if (settings?.app_secret_source === 'environment') {
    secretStatus = t('Configured in environment')
  } else if (settings?.app_secret_source === 'database') {
    secretStatus = t('Configured in database')
  }

  return (
    <section className='bg-card rounded-xl border p-5 shadow-sm'>
      <div className='mb-5 flex flex-wrap items-start justify-between gap-3'>
        <div className='space-y-1'>
          <h4 className='text-base font-semibold'>
            {t('External Game Integration')}
          </h4>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Connect the standalone WTFiB service for New API sign-in and quota transfers.'
            )}
          </p>
        </div>
        <Badge variant={settings?.enabled ? 'default' : 'secondary'}>
          {settings?.enabled ? t('Enabled') : t('Disabled')}
        </Badge>
      </div>

      {settingsQuery.isError ? (
        <Alert variant='destructive'>
          <AlertTitle>{t('Failed to load external game settings')}</AlertTitle>
          <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
            <span>
              {settingsQuery.error instanceof Error
                ? settingsQuery.error.message
                : t('Please try again later.')}
            </span>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={() => settingsQuery.refetch()}
            >
              {t('Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : (
        <Form {...form}>
          <SettingsForm
            onSubmit={form.handleSubmit(onSubmit)}
            autoComplete='off'
          >
            <SettingsPageFormActions
              onSave={form.handleSubmit(onSubmit)}
              isSaving={mutation.isPending || isSubmitting}
              isSaveDisabled={!isDirty || isBusy || !settings}
              saveLabel='Save external game settings'
            />

            {settings?.environment_managed ? (
              <Alert>
                <AlertTitle>
                  {t('Some settings are managed by environment variables')}
                </AlertTitle>
                <AlertDescription>
                  {t(
                    'Remove the corresponding environment variable and restart the service before editing those fields here.'
                  )}
                </AlertDescription>
              </Alert>
            ) : null}

            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>
                      {t('Enable external game integration')}
                    </FormLabel>
                    <FormDescription>
                      {t(
                        'Allow the configured application to authorize users and perform signed quota transfers.'
                      )}{' '}
                      {overrides.enabled ? (
                        <EnvironmentHint name='EXTERNAL_GAME_ENABLED' />
                      ) : null}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={isBusy || overrides.enabled}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />

            <FormField
              control={form.control}
              name='app_id'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Application ID')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='wtfib'
                      {...field}
                      disabled={isBusy || overrides.app_id}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Must match NEW_API_APP_ID in the WTFiB deployment.')}{' '}
                    {overrides.app_id ? (
                      <EnvironmentHint name='EXTERNAL_GAME_APP_ID' />
                    ) : null}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='app_secret'
              render={({ field }) => (
                <FormItem>
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <FormLabel>{t('Application secret')}</FormLabel>
                    <Badge variant='outline'>{secretStatus}</Badge>
                  </div>
                  <FormControl>
                    <Input
                      type='password'
                      autoComplete='new-password'
                      placeholder={
                        settings?.app_secret_configured
                          ? t('Leave blank to keep the existing secret')
                          : t('Enter a secret with at least 16 characters')
                      }
                      {...field}
                      disabled={isBusy || overrides.app_secret}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'The current secret is never returned to the browser. Enter a new value only when rotating it.'
                    )}{' '}
                    {overrides.app_secret ? (
                      <EnvironmentHint name='EXTERNAL_GAME_APP_SECRET' />
                    ) : null}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='redirect_uri'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Redirect URI')}</FormLabel>
                  <FormControl>
                    <Input
                      type='url'
                      placeholder='https://stocks.example.com/login'
                      {...field}
                      disabled={isBusy || overrides.redirect_uri}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'New API sends the one-time authorization code back to this WTFiB login URL.'
                    )}{' '}
                    {overrides.redirect_uri ? (
                      <EnvironmentHint name='EXTERNAL_GAME_REDIRECT_URI' />
                    ) : null}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='code_ttl_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Authorization code TTL (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={30}
                      max={600}
                      {...field}
                      disabled={isBusy || overrides.code_ttl_seconds}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Allowed range: 30-600 seconds. Recommended: 120.')}{' '}
                    {overrides.code_ttl_seconds ? (
                      <EnvironmentHint name='EXTERNAL_GAME_CODE_TTL_SECONDS' />
                    ) : null}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='signature_tolerance_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Signature tolerance (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={30}
                      max={600}
                      {...field}
                      disabled={isBusy || overrides.signature_tolerance_seconds}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Maximum accepted clock difference for signed server requests. Recommended: 300.'
                    )}{' '}
                    {overrides.signature_tolerance_seconds ? (
                      <EnvironmentHint name='EXTERNAL_GAME_SIGNATURE_TOLERANCE_SECONDS' />
                    ) : null}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsForm>
        </Form>
      )}
    </section>
  )
}
