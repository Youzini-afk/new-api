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
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { DateTimePicker } from '@/components/datetime-picker'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
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
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'
import dayjs from '@/lib/dayjs'
import { formatTimestampToDate } from '@/lib/format'

import {
  getCurrentLogCleanupTask,
  getSystemTask,
  startLogCleanupTask,
} from '../api'
import {
  SettingsControlGroup,
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { LogCleanupCategoryState, LogCleanupTask } from '../types'
import { safeNumberFieldProps } from '../utils/numeric-field'

// react-hook-form 7 treats dotted `name` strings as nested paths. The server
// keys here use dots (log_retention.*, log_screening.*), so we model the form
// with nested objects and flatten back to the server-side key format right
// before persisting — same approach as the Performance section.
const logSettingsSchema = z.object({
  LogConsumeEnabled: z.boolean(),
  log_retention: z.object({
    usage_log_retention_days: z.coerce.number().int().min(0),
    error_log_retention_days: z.coerce.number().int().min(0),
    cleanup_interval_hours: z.coerce.number().int().min(1),
  }),
  log_screening: z.object({
    expire_days: z.coerce.number().int().min(7),
  }),
})

type LogSettingsFormInput = z.input<typeof logSettingsSchema>
type LogSettingsFormValues = z.output<typeof logSettingsSchema>

type LogSettingsDefaults = {
  LogConsumeEnabled: boolean
  'log_retention.usage_log_retention_days': number
  'log_retention.error_log_retention_days': number
  'log_retention.cleanup_interval_hours': number
  'log_screening.expire_days': number
}

type LogSettingsSectionProps = {
  defaultValues: LogSettingsDefaults
}

const buildFormDefaults = (
  defaults: LogSettingsDefaults
): LogSettingsFormInput => ({
  LogConsumeEnabled: defaults.LogConsumeEnabled,
  log_retention: {
    usage_log_retention_days:
      defaults['log_retention.usage_log_retention_days'],
    error_log_retention_days:
      defaults['log_retention.error_log_retention_days'],
    cleanup_interval_hours: defaults['log_retention.cleanup_interval_hours'],
  },
  log_screening: {
    expire_days: defaults['log_screening.expire_days'],
  },
})

const flattenFormValues = (
  values: LogSettingsFormValues
): LogSettingsDefaults => ({
  LogConsumeEnabled: values.LogConsumeEnabled,
  'log_retention.usage_log_retention_days':
    values.log_retention.usage_log_retention_days,
  'log_retention.error_log_retention_days':
    values.log_retention.error_log_retention_days,
  'log_retention.cleanup_interval_hours':
    values.log_retention.cleanup_interval_hours,
  'log_screening.expire_days': values.log_screening.expire_days,
})

type ServerLogInfo = {
  enabled: boolean
  log_dir: string
  file_count: number
  total_size: number
  oldest_time?: string
  newest_time?: string
}

const HOURS_IN_DAY = 24

function formatBytes(bytes: number, decimals = 2): string {
  if (!bytes || Number.isNaN(bytes)) return '0 Bytes'
  if (bytes === 0) return '0 Bytes'
  if (bytes < 0) return `-${formatBytes(-bytes, decimals)}`
  const k = 1024
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k))
  if (i < 0 || i >= sizes.length) return `${bytes} Bytes`
  return `${Number.parseFloat((bytes / Math.pow(k, i)).toFixed(decimals))} ${
    sizes[i]
  }`
}

const getDateHoursAgo = (hours: number) => {
  const date = new Date()
  date.setHours(date.getHours() - hours)
  return date
}

const getDateDaysAgo = (days: number) => getDateHoursAgo(days * HOURS_IN_DAY)

const quickSelectOptions = [
  {
    label: '24 hours ago',
    getValue: () => getDateHoursAgo(24),
  },
  {
    label: '7 days ago',
    getValue: () => getDateDaysAgo(7),
  },
  {
    label: '30 days ago',
    getValue: () => getDateDaysAgo(30),
  },
]

function isActiveLogCleanupTask(task: LogCleanupTask | null) {
  return task?.status === 'pending' || task?.status === 'running'
}

// Category key → i18n label. Backend keys are usage_logs / error_logs /
// screening_records (see service.LogCleanupCategory* constants).
const CATEGORY_LABEL_KEYS: Record<string, string> = {
  usage_logs: 'Usage Logs',
  error_logs: 'Error Insight Logs',
  screening_records: 'Screening Records',
}

// Reason code → i18n label. Currently only ClickHouse-managed TTL is surfaced.
const REASON_LABEL_KEYS: Record<string, string> = {
  managed_by_clickhouse_ttl: 'Skipped (managed by ClickHouse TTL)',
}

type LogCleanupCategoryRowProps = {
  categoryKey: string
  state: LogCleanupCategoryState
}

function LogCleanupCategoryRow(props: LogCleanupCategoryRowProps) {
  const { t } = useTranslation()
  const labelKey = CATEGORY_LABEL_KEYS[props.categoryKey] ?? props.categoryKey
  const progress = Math.min(100, Math.max(0, props.state.progress ?? 0))
  const total = props.state.total ?? 0
  const processed = props.state.processed ?? 0
  const deleted = props.state.deleted_count ?? 0
  const cutoff = props.state.cutoff_timestamp
  const reason = props.state.reason
  const reasonLabel = reason ? (REASON_LABEL_KEYS[reason] ?? reason) : null

  return (
    <div className='space-y-1'>
      <div className='flex items-center justify-between gap-2 text-xs'>
        <span className='font-medium'>{t(labelKey)}</span>
        <span className='text-muted-foreground tabular-nums'>{progress}%</span>
      </div>
      <Progress value={progress} />
      <div className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs'>
        <span>
          {t('{{processed}} of {{total}} processed', {
            processed,
            total,
          })}
        </span>
        {deleted > 0 && (
          <span>{t('Deleted: {{count}}', { count: deleted })}</span>
        )}
        {cutoff ? (
          <span>
            {t('Cutoff')}: {formatTimestampToDate(cutoff, 'seconds')}
          </span>
        ) : null}
        {reasonLabel && (
          <span className='text-muted-foreground/80'>
            {t('Reason')}: {t(reasonLabel)}
          </span>
        )}
      </div>
    </div>
  )
}

export function LogSettingsSection(props: LogSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<LogSettingsFormInput, unknown, LogSettingsFormValues>({
    resolver: zodResolver(logSettingsSchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<LogSettingsDefaults>(props.defaultValues)
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

  const [purgeDate, setPurgeDate] = useState<Date | undefined>(() =>
    getDateDaysAgo(30)
  )
  const [isStartingLogCleanup, setIsStartingLogCleanup] = useState(false)
  const [logCleanupTask, setLogCleanupTask] = useState<LogCleanupTask | null>(
    null
  )
  const [showConfirmDialog, setShowConfirmDialog] = useState(false)
  const [serverLogInfo, setServerLogInfo] = useState<ServerLogInfo | null>(null)
  const [serverLogCleanupMode, setServerLogCleanupMode] = useState('by_count')
  const [serverLogCleanupValue, setServerLogCleanupValue] = useState(10)
  const [serverLogCleanupLoading, setServerLogCleanupLoading] = useState(false)

  const fetchServerLogInfo = useCallback(async () => {
    try {
      const res = await api.get('/api/performance/logs')
      if (res.data.success) setServerLogInfo(res.data.data)
    } catch {
      /* ignore */
    }
  }, [])

  useEffect(() => {
    fetchServerLogInfo()
  }, [fetchServerLogInfo])

  useEffect(() => {
    let cancelled = false

    async function fetchCurrentLogCleanupTask() {
      try {
        const res = await getCurrentLogCleanupTask()
        if (!cancelled && res.success && res.data) {
          setLogCleanupTask(res.data)
        }
      } catch {
        /* ignore */
      }
    }

    fetchCurrentLogCleanupTask()

    return () => {
      cancelled = true
    }
  }, [])

  const purgeTimestamp = useMemo(() => {
    if (!purgeDate) return null
    return Math.floor(purgeDate.getTime() / 1000)
  }, [purgeDate])

  const formattedPurgeDate = useMemo(() => {
    if (!purgeDate) return ''
    return formatTimestampToDate(purgeDate.getTime(), 'milliseconds')
  }, [purgeDate])

  const logCleanupActive = isActiveLogCleanupTask(logCleanupTask)
  const logCleanupTaskId = logCleanupTask?.task_id
  const logCleanupTaskStatus = logCleanupTask?.status
  const logCleanupState = logCleanupTask?.state
  const logCleanupProgress = Math.min(
    100,
    Math.max(0, logCleanupState?.progress ?? 0)
  )
  const logCleanupProcessed = logCleanupState?.processed ?? 0
  const logCleanupTotal = logCleanupState?.total ?? 0
  const logCleanupCategories = useMemo(() => {
    const cats = logCleanupState?.categories
    if (!cats) {
      return [] as Array<{ key: string; state: LogCleanupCategoryState }>
    }
    return Object.entries(cats).map(([key, state]) => ({ key, state }))
  }, [logCleanupState?.categories])

  useEffect(() => {
    if (
      !logCleanupTaskId ||
      (logCleanupTaskStatus !== 'pending' && logCleanupTaskStatus !== 'running')
    ) {
      return
    }

    let cancelled = false
    const interval = window.setInterval(async () => {
      try {
        const res = await getSystemTask(logCleanupTaskId)
        if (cancelled || !res.success || !res.data) return

        setLogCleanupTask(res.data)
        if (!isActiveLogCleanupTask(res.data)) {
          if (res.data.status === 'succeeded') {
            const count =
              res.data.result?.deleted_count ?? res.data.state?.processed ?? 0
            toast.success(
              count > 0
                ? t('{{count}} log entries removed.', { count })
                : t('No log entries matched the selected time.')
            )
          } else if (res.data.status === 'failed') {
            toast.error(res.data.error || t('Failed to clean logs'))
          }
        }
      } catch {
        /* keep polling */
      }
    }, 1000)

    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [logCleanupTaskId, logCleanupTaskStatus, t])

  const onSubmit = async (values: LogSettingsFormValues) => {
    const normalized = flattenFormValues(values)
    const changedKeys = (
      Object.keys(normalized) as Array<keyof LogSettingsDefaults>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      const res = await updateOption.mutateAsync({
        key,
        value: normalized[key],
      })
      if (!res.success) {
        return
      }
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
    form.reset(buildFormDefaults(normalized))
  }

  const handleRequestCleanLogs = () => {
    if (!purgeTimestamp) {
      toast.error(t('Select a timestamp before clearing logs.'))
      return
    }

    setShowConfirmDialog(true)
  }

  const handleCleanLogs = async () => {
    if (!purgeTimestamp) {
      toast.error(t('Select a timestamp before clearing logs.'))
      return
    }

    setIsStartingLogCleanup(true)
    try {
      const res = await startLogCleanupTask(purgeTimestamp)
      if (!res.success) {
        throw new Error(res.message || t('Failed to clean logs'))
      }
      if (!res.data) {
        throw new Error(t('Failed to clean logs'))
      }
      setLogCleanupTask(res.data)
      setShowConfirmDialog(false)
      toast.success(t('Log cleanup task started.'))
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Failed to clean logs')
      toast.error(message)
    } finally {
      setIsStartingLogCleanup(false)
    }
  }

  const cleanupServerLogFiles = async () => {
    if (
      !serverLogCleanupValue ||
      Number.isNaN(serverLogCleanupValue) ||
      serverLogCleanupValue < 1
    ) {
      toast.error(t('Please enter a valid number'))
      return
    }

    setServerLogCleanupLoading(true)
    try {
      const res = await api.delete(
        `/api/performance/logs?mode=${serverLogCleanupMode}&value=${serverLogCleanupValue}`
      )
      if (res.data.success) {
        const { deleted_count, freed_bytes } = res.data.data
        toast.success(
          t('Cleaned up {{count}} log files, freed {{size}}', {
            count: deleted_count,
            size: formatBytes(freed_bytes),
          })
        )
      } else {
        toast.error(res.data.message || t('Cleanup failed'))
      }
      fetchServerLogInfo()
    } catch {
      toast.error(t('Cleanup failed'))
    } finally {
      setServerLogCleanupLoading(false)
    }
  }

  return (
    <SettingsSection title={t('Log Maintenance')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save log settings'
          />
          <FormField
            control={form.control}
            name='LogConsumeEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Record quota usage')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Track per-request consumption to power usage analytics. Keeping this on increases database writes.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </SettingsSwitchItem>
            )}
          />

          <SettingsControlGroup className='space-y-3'>
            <div>
              <h4 className='text-sm font-medium'>
                {t('Automatic retention')}
              </h4>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Automatically delete old log records on a schedule. Set retention days to 0 to disable cleanup for that category.'
                )}
              </p>
            </div>
            <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='log_retention.usage_log_retention_days'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Usage log retention (days)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('0 = disabled for that category.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='log_retention.error_log_retention_days'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Error insight log retention (days)')}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('0 = disabled for that category.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='log_screening.expire_days'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Screening record retention (days)')}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={7}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Screening records use the existing log screening retention with a minimum of 7 days.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='log_retention.cleanup_interval_hours'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Cleanup interval (hours)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Automatic cleanup runs every {{hours}} hours and removes records older than the configured retention.',
                        { hours: field.value || 1 }
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
            <Alert>
              <AlertDescription>
                {t(
                  'Deletion is permanent; removed records cannot be recovered.'
                )}{' '}
                {t(
                  'Note: usage log scheduled cleanup may be skipped or managed by the database TTL (e.g. ClickHouse) when applicable.'
                )}
              </AlertDescription>
            </Alert>
          </SettingsControlGroup>

          <SettingsControlGroup className='space-y-3'>
            <div>
              <h4 className='text-sm font-medium'>{t('Clean history logs')}</h4>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Manual cleanup only removes usage/call logs created before the selected timestamp.'
                )}
              </p>
            </div>
            <DateTimePicker value={purgeDate} onChange={setPurgeDate} />
            <div className='flex flex-wrap gap-3'>
              {quickSelectOptions.map((option) => (
                <Button
                  key={option.label}
                  type='button'
                  variant='outline'
                  onClick={() => setPurgeDate(option.getValue())}
                >
                  {t(option.label)}
                </Button>
              ))}
              <Button
                type='button'
                variant='destructive'
                onClick={handleRequestCleanLogs}
                disabled={isStartingLogCleanup || logCleanupActive}
              >
                {isStartingLogCleanup || logCleanupActive
                  ? t('Cleaning...')
                  : t('Clean logs')}
              </Button>
            </div>
            {logCleanupTask && (
              <div className='rounded-md border p-3'>
                <div className='mb-2 flex items-center justify-between gap-3 text-sm'>
                  <span className='font-medium'>
                    {t('Log cleanup progress')}
                  </span>
                  <span className='text-muted-foreground tabular-nums'>
                    {logCleanupProgress}%
                  </span>
                </div>
                <Progress value={logCleanupProgress} />
                <div className='text-muted-foreground mt-2 text-xs'>
                  {t('{{processed}} of {{total}} log entries processed.', {
                    processed: logCleanupProcessed,
                    total: logCleanupTotal,
                  })}
                </div>
                {logCleanupTask.status === 'failed' && logCleanupTask.error && (
                  <div className='text-destructive mt-2 text-xs'>
                    {logCleanupTask.error}
                  </div>
                )}
                {logCleanupCategories.length > 0 && (
                  <div className='mt-3 space-y-3 border-t pt-3'>
                    <div className='text-xs font-medium'>
                      {t('Category progress')}
                    </div>
                    {logCleanupCategories.map((entry) => (
                      <LogCleanupCategoryRow
                        key={entry.key}
                        categoryKey={entry.key}
                        state={entry.state}
                      />
                    ))}
                  </div>
                )}
              </div>
            )}
          </SettingsControlGroup>
        </SettingsForm>
      </Form>

      <Separator />

      <div className='space-y-4'>
        <div>
          <h4 className='font-medium'>{t('Server Log Management')}</h4>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t(
              'Manage server log files. Log files accumulate over time; regular cleanup is recommended to free disk space.'
            )}
          </p>
        </div>

        {serverLogInfo !== null &&
          (serverLogInfo.enabled ? (
            <div className='space-y-4'>
              <div className='rounded-lg border p-4'>
                <div className='grid grid-cols-2 gap-2 text-sm md:grid-cols-4'>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Log Directory')}:
                    </span>{' '}
                    <span className='font-mono text-xs'>
                      {serverLogInfo.log_dir}
                    </span>
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Log File Count')}:
                    </span>{' '}
                    {serverLogInfo.file_count}
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Total Log Size')}:
                    </span>{' '}
                    {formatBytes(serverLogInfo.total_size)}
                  </div>
                  {serverLogInfo.oldest_time && serverLogInfo.newest_time && (
                    <div>
                      <span className='text-muted-foreground'>
                        {t('Date Range')}:
                      </span>{' '}
                      {dayjs(serverLogInfo.oldest_time).format('YYYY-MM-DD')} ~{' '}
                      {dayjs(serverLogInfo.newest_time).format('YYYY-MM-DD')}
                    </div>
                  )}
                </div>
              </div>

              <div className='flex flex-wrap items-end gap-3'>
                <div className='grid gap-1.5'>
                  <Label className='text-xs'>{t('Cleanup Mode')}</Label>
                  <Select
                    items={[
                      { value: 'by_count', label: t('Retain last N files') },
                      { value: 'by_days', label: t('Retain last N days') },
                    ]}
                    value={serverLogCleanupMode}
                    onValueChange={(value) =>
                      value !== null && setServerLogCleanupMode(value)
                    }
                  >
                    <SelectTrigger className='w-[160px]'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='by_count'>
                          {t('Retain last N files')}
                        </SelectItem>
                        <SelectItem value='by_days'>
                          {t('Retain last N days')}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
                <div className='grid gap-1.5'>
                  <Label className='text-xs'>
                    {serverLogCleanupMode === 'by_count'
                      ? t('Files to Retain')
                      : t('Days to Retain')}
                  </Label>
                  <Input
                    type='number'
                    min={1}
                    max={serverLogCleanupMode === 'by_count' ? 1000 : 3650}
                    value={serverLogCleanupValue}
                    onChange={(event) =>
                      setServerLogCleanupValue(Number(event.target.value))
                    }
                    className='w-[120px]'
                  />
                </div>
                <AlertDialog>
                  <AlertDialogTrigger
                    render={
                      <Button
                        type='button'
                        variant='destructive'
                        size='sm'
                        disabled={serverLogCleanupLoading}
                      />
                    }
                  >
                    {serverLogCleanupLoading
                      ? t('Cleaning...')
                      : t('Clean Up Log Files')}
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>
                        {t('Confirm log file cleanup?')}
                      </AlertDialogTitle>
                      <AlertDialogDescription>
                        {serverLogCleanupMode === 'by_count'
                          ? t(
                              'Only the last {{value}} log files will be retained; the rest will be deleted.',
                              {
                                value: serverLogCleanupValue,
                              }
                            )
                          : t(
                              'Log files older than {{value}} days will be deleted.',
                              {
                                value: serverLogCleanupValue,
                              }
                            )}
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
                      <AlertDialogAction onClick={cleanupServerLogFiles}>
                        {t('Confirm Cleanup')}
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </div>
            </div>
          ) : (
            <Alert>
              <AlertDescription>
                {t(
                  'Server logging is not enabled (log directory not configured)'
                )}
              </AlertDescription>
            </Alert>
          ))}
      </div>

      <AlertDialog open={showConfirmDialog} onOpenChange={setShowConfirmDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Confirm usage log cleanup')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {formattedPurgeDate
                ? t(
                    'This will permanently remove usage/call log entries created before {{date}}.',
                    { date: formattedPurgeDate }
                  )
                : t(
                    'This will permanently remove usage/call log entries before the selected timestamp.'
                  )}{' '}
              {t('This action cannot be undone.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isStartingLogCleanup}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleCleanLogs}
              disabled={isStartingLogCleanup}
            >
              {isStartingLogCleanup ? t('Cleaning...') : t('Delete logs')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
