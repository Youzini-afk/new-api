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
/**
 * Shared right-side drawer for inspecting a prompt / UA block log in detail,
 * including the masked raw request headers/params and an auto-ban summary.
 */
import { useTranslation } from 'react-i18next'
import { Ban, MessageSquareWarning, ShieldCheck } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
  DrawerDescription,
} from '@/components/ui/drawer'
import { CopyButton } from '@/components/copy-button'
import { cn } from '@/lib/utils'
import { RawRequestViewer } from './raw-request-viewer'
import {
  formatScreeningNumber,
  formatScreeningTimestamp,
} from '../lib/utils'
import type { PromptBlockLogDetail, UABlockLogDetail } from '../types'

export type BlockLogKind = 'prompt' | 'ua'

export type BlockLogDetail = PromptBlockLogDetail | UABlockLogDetail

interface BlockLogDetailDrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  kind: BlockLogKind
  detail: BlockLogDetail | null
  isLoading: boolean
  onAddRemark: () => void
}

export function BlockLogDetailDrawer(props: BlockLogDetailDrawerProps) {
  const { t } = useTranslation()
  const { kind, detail } = props

  const isUa = kind === 'ua'

  const renderDetailBody = () => {
    if (props.isLoading) {
      return (
        <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
      )
    }
    if (!detail) {
      return <p className='text-muted-foreground text-sm'>{t('No data')}</p>
    }
    return (
      <div className='flex flex-col gap-4'>
        <UserCard
          username={detail.username || `ID: ${detail.user_id}`}
          displayName={detail.display_name}
          userId={detail.user_id}
          ip={detail.ip}
          currentRemark={detail.remark}
        />

        <AutoBanSummary
          configured={detail.auto_ban_configured}
          banned={detail.auto_banned}
          reason={detail.ban_reason}
        />

        <FieldGrid>
          <Field label={t('Rule pattern')} mono>
            {detail.rule_pattern || t('N/A')}
          </Field>
          <Field label={t('Rule message')}>
            {detail.rule_message || t('N/A')}
          </Field>
          <Field label={t('Error code')} mono>
            {detail.error_code || t('N/A')}
          </Field>
          <Field label={t('HTTP status')}>
            {formatScreeningNumber(detail.http_status_code)}
          </Field>
          <Field label={t('Request path')} mono>
            {detail.request_path || t('N/A')}
          </Field>
          {isUa ? (
            <UASpecificFields detail={detail as UABlockLogDetail} />
          ) : (
            <Field label={t('Match mode')}>
              {(detail as PromptBlockLogDetail).match_mode || t('N/A')}
            </Field>
          )}
          {isUa && (
            <Field label={t('User agent')} mono>
              <div className='flex items-start gap-2'>
                <span className='break-all'>
                  {(detail as UABlockLogDetail).user_agent || t('Empty')}
                </span>
                {(detail as UABlockLogDetail).user_agent && (
                  <CopyButton
                    value={(detail as UABlockLogDetail).user_agent}
                    size='icon'
                    variant='ghost'
                    tooltip={t('Copy User Agent')}
                  />
                )}
              </div>
            </Field>
          )}
        </FieldGrid>

        <RawRequestViewer
          headersRaw={detail.request_headers_raw}
          paramsRaw={detail.request_params_raw}
        />
      </div>
    )
  }

  return (
    <Drawer open={props.open} onOpenChange={props.onOpenChange} direction='right'>
      <DrawerContent className='w-full sm:max-w-lg'>
        <DrawerHeader className='border-b'>
          <DrawerTitle className='truncate'>
            {isUa ? t('UA Block Detail') : t('Prompt Block Detail')}
          </DrawerTitle>
          <DrawerDescription className='text-muted-foreground text-xs'>
            {detail
              ? t('Matched at {{time}}', {
                  time: formatScreeningTimestamp(detail.matched_at),
                })
              : ''}
          </DrawerDescription>
        </DrawerHeader>

        <div className='min-h-0 flex-1 overflow-auto p-4'>
          {renderDetailBody()}
        </div>

        <DrawerFooter className='border-t'>
          <Button
            variant='secondary'
            disabled={!detail}
            onClick={props.onAddRemark}
          >
            <MessageSquareWarning className='size-4' />
            {t('Append remark')}
          </Button>
          <DrawerClose asChild>
            <Button variant='outline'>{t('Close')}</Button>
          </DrawerClose>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  )
}

function UserCard(props: {
  username: string
  displayName?: string
  userId: number
  ip: string
  currentRemark?: string
}) {
  const { t } = useTranslation()
  return (
    <div className='bg-muted/40 rounded-lg border p-3'>
      <div className='flex items-center justify-between gap-2'>
        <div className='flex flex-col'>
          <span className='text-sm font-medium'>{props.username}</span>
          {props.displayName && (
            <span className='text-muted-foreground text-xs'>
              {props.displayName}
            </span>
          )}
        </div>
        <span className='text-muted-foreground text-xs'>
          {t('User ID')}: {props.userId}
        </span>
      </div>
      {props.ip && (
        <div className='mt-2 flex items-center gap-2'>
          <span className='text-muted-foreground text-xs'>{t('IP')}:</span>
          <span className='font-mono text-xs'>{props.ip}</span>
          <CopyButton value={props.ip} size='icon' variant='ghost' />
        </div>
      )}
      {props.currentRemark && (
        <p className='text-muted-foreground mt-2 text-xs whitespace-pre-wrap break-words'>
          {props.currentRemark}
        </p>
      )}
    </div>
  )
}

function AutoBanSummary(props: {
  configured: boolean
  banned: boolean
  reason: string
}) {
  const { t } = useTranslation()
  if (!props.configured) {
    return (
      <div className='text-muted-foreground flex items-center gap-2 text-xs'>
        <ShieldCheck className='size-3.5' />
        {t('Auto-ban is not configured for this rule.')}
      </div>
    )
  }
  return (
    <div className='flex flex-col gap-1 rounded-lg border border-orange-500/30 bg-orange-500/10 p-3'>
      <div className='flex items-center gap-2'>
        <Ban className='size-4 text-orange-600 dark:text-orange-400' />
        <span className='text-sm font-medium'>
          {t('Local auto-ban configured')}
        </span>
        <Badge
          variant={props.banned ? 'destructive' : 'secondary'}
          className='ml-auto'
        >
          {props.banned ? t('Banned') : t('Not banned')}
        </Badge>
      </div>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Auto-ban disables the user and their tokens locally. It does not perform any external joint ban.'
        )}
      </p>
      {props.reason && (
        <p className='mt-1 text-xs whitespace-pre-wrap break-words'>
          {props.reason}
        </p>
      )}
    </div>
  )
}

function FieldGrid(props: { children: React.ReactNode }) {
  return (
    <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>{props.children}</div>
  )
}

function Field(props: {
  label: string
  mono?: boolean
  children: React.ReactNode
}) {
  return (
    <div className='flex flex-col gap-1'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className={cn('text-sm break-words', props.mono && 'font-mono text-xs')}>
        {props.children}
      </span>
    </div>
  )
}

function UASpecificFields(props: { detail: UABlockLogDetail }) {
  const { t } = useTranslation()
  return (
    <Field label={t('Empty UA')}>
      {props.detail.is_empty_ua ? t('Yes') : t('No')}
    </Field>
  )
}
