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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestamp } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { recheckOwnDiscordGate } from '../api'
import type { UserProfile } from '../types'

interface DiscordGateStatusCardProps {
  profile: UserProfile | null
  onProfileUpdate: () => void
}

function getDiscordGateStatus(
  profile: UserProfile | null
): { labelKey: string; variant: StatusVariant } {
  if (!profile?.discord_id) {
    return { labelKey: 'Not linked', variant: 'neutral' }
  }
  if (profile.discord_gate_exempt) {
    return { labelKey: 'Exempt', variant: 'info' }
  }
  if (profile.discord_gate_passed) {
    return { labelKey: 'Passed', variant: 'success' }
  }
  if (profile.discord_last_check_result) {
    return { labelKey: 'Failed', variant: 'danger' }
  }
  return { labelKey: 'Not checked', variant: 'warning' }
}

export function DiscordGateStatusCard(props: DiscordGateStatusCardProps) {
  const { t } = useTranslation()
  const [isRechecking, setIsRechecking] = useState(false)
  const status = getDiscordGateStatus(props.profile)

  const handleRecheck = async () => {
    setIsRechecking(true)
    try {
      const result = await recheckOwnDiscordGate()
      if (result.success) {
        toast.success(t('Discord eligibility rechecked'))
        props.onProfileUpdate()
      } else {
        toast.error(result.message || t('Failed to recheck Discord eligibility'))
      }
    } catch {
      toast.error(t('Failed to recheck Discord eligibility'))
    } finally {
      setIsRechecking(false)
    }
  }

  if (!props.profile) return null

  return (
    <div className='mt-4 rounded-lg border p-3'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
        <div className='space-y-2'>
          <div className='flex items-center gap-2'>
            <p className='text-sm font-medium'>{t('Discord Gate')}</p>
            <StatusBadge
              label={t(status.labelKey)}
              variant={status.variant}
              copyable={false}
            />
          </div>
          <div className='text-muted-foreground grid gap-1 text-xs sm:grid-cols-2'>
            <span>
              {t('Discord linked:')}{' '}
              {props.profile.discord_id ? t('Yes') : t('No')}
            </span>
            <span>
              {t('Last check:')}{' '}
              {props.profile.discord_last_check_at
                ? formatTimestamp(props.profile.discord_last_check_at)
                : t('Never')}
            </span>
            <span>
              {t('Result:')}{' '}
              {props.profile.discord_last_check_result || t('Not checked')}
            </span>
            <span>
              {t('Reason:')}{' '}
              {props.profile.discord_last_check_reason || t('None')}
            </span>
          </div>
          {props.profile.discord_gate_message && (
            <p className='text-muted-foreground text-xs'>
              {t('Message:')} {props.profile.discord_gate_message}
            </p>
          )}
        </div>
        <Button
          variant='outline'
          size='sm'
          onClick={handleRecheck}
          disabled={isRechecking || !props.profile.discord_id}
          className='shrink-0'
        >
          {isRechecking ? t('Rechecking...') : t('Recheck Discord eligibility')}
        </Button>
      </div>
    </div>
  )
}
