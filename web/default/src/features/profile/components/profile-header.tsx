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
import { Activity, BarChart3, WalletCards, RefreshCw } from 'lucide-react'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { IconDiscord } from '@/assets/brand-icons/icon-discord'
import { StatusBadge } from '@/components/status-badge'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import dayjs from '@/lib/dayjs'
import { formatCompactNumber, formatQuota } from '@/lib/format'
import { getRoleLabel } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { syncDiscordAvatar } from '../api'
import { getDisplayName } from '../lib'
import type { UserProfile } from '../types'
import { AvatarEditor } from './avatar-editor'

// ============================================================================
// Profile Header Component
// ============================================================================

interface ProfileHeaderProps {
  profile: UserProfile | null
  loading: boolean
  onProfileUpdate: () => void
}

export function ProfileHeader({
  profile,
  loading,
  onProfileUpdate,
}: ProfileHeaderProps) {
  const { t } = useTranslation()
  const setUser = useAuthStore((state) => state.auth.setUser)
  const [syncingDiscord, setSyncingDiscord] = useState(false)
  const [showOverwriteAlert, setShowOverwriteAlert] = useState(false)

  // Patch the auth-store user's avatar so the dropdown/drawer avatar updates
  // instantly after a successful Discord sync, mirroring AvatarEditor.
  const syncAuthUserAvatar = useCallback(
    (patch: { avatar_url?: string; avatar_source?: string }) => {
      const currentUser = useAuthStore.getState().auth.user
      if (!currentUser) return
      setUser({
        ...currentUser,
        avatar_url: patch.avatar_url,
        avatar_source: patch.avatar_source,
      })
    },
    [setUser]
  )

  const handleDiscordSync = async (force: boolean = false) => {
    if (!profile) return

    if (!force && profile.avatar_source === 'uploaded') {
      setShowOverwriteAlert(true)
      return
    }

    try {
      setSyncingDiscord(true)
      const res = await syncDiscordAvatar(force)
      if (res.success) {
        if (res.data?.synced) {
          toast.success(t('Discord avatar synced successfully'))
          // Keep the top-bar avatar in sync with the newly stored avatar.
          syncAuthUserAvatar({
            avatar_url: res.data.avatar_url,
            avatar_source: res.data.avatar_source,
          })
          onProfileUpdate()
        } else if (res.data?.skipped) {
          const reason = res.data.reason
          switch (reason) {
            case 'uploaded_avatar_protected':
              setShowOverwriteAlert(true)
              break
            case 'missing_discord_binding':
              toast.error(t('Please bind your Discord account first'))
              break
            case 'missing_discord_avatar':
              toast.error(t('No Discord avatar found'))
              break
            case 'download_failed':
              toast.error(t('Failed to download Discord avatar'))
              break
            case 'invalid_image':
              toast.error(t('Invalid Discord avatar format'))
              break
            case 'unchanged':
              toast.success(t('Discord avatar is already up to date'))
              break
            default:
              toast.info(t('Discord avatar sync skipped'))
          }
        }
      } else {
        switch (res.data?.reason) {
          case 'missing_discord_binding':
            toast.error(t('Please bind your Discord account first'))
            break
          case 'missing_discord_avatar':
            toast.error(t('No Discord avatar found'))
            break
          case 'download_failed':
            toast.error(t('Failed to download Discord avatar'))
            break
          case 'invalid_image':
            toast.error(t('Invalid Discord avatar format'))
            break
          default:
            toast.error(t('Failed to sync Discord avatar'))
        }
      }
    } catch {
      toast.error(t('Failed to sync Discord avatar'))
    } finally {
      setSyncingDiscord(false)
    }
  }

  if (loading) {
    return (
      <Card data-card-hover='false' className='gap-0 overflow-hidden py-0'>
        <CardContent className='p-4 sm:p-5'>
          <div className='flex flex-col items-center gap-4 text-center sm:flex-row sm:text-left'>
            <Skeleton className='h-16 w-16 rounded-2xl' />
            <div className='space-y-3'>
              <div className='flex flex-col items-center gap-2 sm:flex-row sm:justify-start'>
                <Skeleton className='h-8 w-48' />
                <Skeleton className='h-5 w-16' />
              </div>
              <div className='flex flex-col items-center gap-1 sm:flex-row sm:justify-start sm:gap-4'>
                <Skeleton className='h-4 w-24' />
                <Skeleton className='h-4 w-40' />
                <Skeleton className='h-4 w-20' />
              </div>
            </div>
          </div>
        </CardContent>
        <div className='border-t'>
          <div className='divide-border/60 grid grid-cols-1 divide-y sm:grid-cols-3 sm:divide-x sm:divide-y-0'>
            {['balance', 'usage', 'requests'].map((key) => (
              <div key={key} className='px-4 py-3.5 sm:px-5 sm:py-4'>
                <Skeleton className='h-3.5 w-20' />
                <Skeleton className='mt-2 h-7 w-28' />
                <Skeleton className='mt-1.5 h-3.5 w-24' />
              </div>
            ))}
          </div>
        </div>
      </Card>
    )
  }

  if (!profile) return null

  const displayName = getDisplayName(profile)
  const roleLabel = getRoleLabel(profile.role)
  const stats = [
    {
      label: t('Current Balance'),
      value: formatQuota(profile.quota),
      description: t('Remaining quota'),
      icon: WalletCards,
    },
    {
      label: t('Total Usage'),
      value: formatQuota(profile.used_quota),
      description: t('Total consumed quota'),
      icon: BarChart3,
    },
    {
      label: t('API Requests'),
      value: formatCompactNumber(profile.request_count),
      description: t('Total requests made'),
      icon: Activity,
    },
  ]
  let discordAvatarButtonLabel = t('Sync Discord Avatar')
  if (profile.avatar_source === 'uploaded') {
    discordAvatarButtonLabel = t('Replace with Discord Avatar')
  } else if (profile.avatar_source === 'discord') {
    discordAvatarButtonLabel = t('Re-sync Discord Avatar')
  }
  let discordDisplayName = profile.discord_id || ''
  if (profile.discord_global_name) {
    discordDisplayName = profile.discord_username
      ? `${profile.discord_global_name} (@${profile.discord_username})`
      : profile.discord_global_name
  } else if (profile.discord_username) {
    discordDisplayName = `@${profile.discord_username}`
    if (
      profile.discord_discriminator &&
      profile.discord_discriminator !== '0'
    ) {
      discordDisplayName = `${discordDisplayName}#${profile.discord_discriminator}`
    }
  }
  const discordProfileSyncedText = profile.discord_profile_synced_at
    ? t('Discord profile synced {{time}}', {
        time: dayjs(profile.discord_profile_synced_at * 1000).format(
          'YYYY-MM-DD'
        ),
      })
    : ''

  return (
    <Card data-card-hover='false' className='gap-0 overflow-hidden py-0'>
      <CardContent className='p-3 sm:p-5'>
        <div className='flex items-center gap-3 text-left sm:gap-4'>
          <AvatarEditor profile={profile} onProfileUpdate={onProfileUpdate} />

          <div className='min-w-0 flex-1 space-y-1.5 sm:space-y-3'>
            <div className='flex min-w-0 items-center gap-2'>
              <h1 className='truncate text-xl font-semibold tracking-tight sm:text-2xl'>
                {displayName}
              </h1>
              <StatusBadge
                label={roleLabel}
                variant='neutral'
                copyable={false}
              />
              <StatusBadge
                label={`${t('User ID')} ${profile.id}`}
                variant='info'
                copyText={String(profile.id)}
              />
            </div>

            <div className='text-muted-foreground flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs sm:gap-x-4 sm:text-sm'>
              <span className='truncate'>@{profile.username}</span>
              {profile.email && (
                <>
                  <span>•</span>
                  <span className='truncate'>{profile.email}</span>
                </>
              )}
              {profile.group && (
                <>
                  <span>•</span>
                  <span className='truncate'>{profile.group}</span>
                </>
              )}
            </div>

            {profile.discord_id && (
              <div className='mt-2 flex flex-col gap-1.5'>
                <div className='flex flex-wrap items-center gap-2'>
                  <div className='inline-flex max-w-full min-w-0 items-center gap-1.5 rounded-full bg-[#5865F2]/10 px-2.5 py-1 text-xs font-medium text-[#5865F2]'>
                    <IconDiscord className='size-3.5 shrink-0' />
                    <span className='min-w-0 truncate'>
                      {discordDisplayName}
                    </span>
                  </div>
                  <AvatarSourceBadge source={profile.avatar_source} />
                  <Button
                    variant='outline'
                    size='sm'
                    className='h-7 rounded-full text-xs'
                    disabled={syncingDiscord}
                    onClick={() => handleDiscordSync(false)}
                  >
                    <RefreshCw
                      className={`mr-1.5 size-3 ${syncingDiscord ? 'animate-spin' : ''}`}
                    />
                    {discordAvatarButtonLabel}
                  </Button>
                </div>
                {discordProfileSyncedText ? (
                  <div className='text-muted-foreground text-xs'>
                    {discordProfileSyncedText}
                  </div>
                ) : null}
              </div>
            )}
          </div>
        </div>
      </CardContent>
      <div className='border-t'>
        <div className='divide-border/60 grid grid-cols-3 divide-x'>
          {stats.map((item) => (
            <div key={item.label} className='min-w-0 px-3 py-3 sm:px-5 sm:py-4'>
              <div className='flex items-center gap-2'>
                <item.icon className='text-muted-foreground/60 size-3.5 shrink-0' />
                <div className='text-muted-foreground truncate text-xs font-medium tracking-wider uppercase'>
                  {item.label}
                </div>
              </div>

              <div className='text-foreground mt-1.5 truncate font-mono text-lg font-bold tracking-tight tabular-nums sm:mt-2 sm:text-2xl'>
                {item.value}
              </div>
              <div className='text-muted-foreground/60 mt-1 hidden text-xs md:block'>
                {item.description}
              </div>
            </div>
          ))}
        </div>
      </div>

      <AlertDialog
        open={showOverwriteAlert}
        onOpenChange={setShowOverwriteAlert}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Overwrite Uploaded Avatar?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'You currently have a custom uploaded avatar. Syncing from Discord will overwrite it. Do you want to continue?'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={syncingDiscord}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={syncingDiscord}
              onClick={(e) => {
                e.preventDefault()
                setShowOverwriteAlert(false)
                handleDiscordSync(true)
              }}
            >
              {syncingDiscord ? (
                <RefreshCw className='mr-2 size-4 animate-spin' />
              ) : null}
              {t('Overwrite')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}

// ============================================================================
// Avatar Source Badge
// ============================================================================
// Small inline pill that surfaces where the currently displayed avatar comes
// from ("uploaded", "discord", or "default"), so users understand the active
// avatar source before triggering a Discord sync.
// ============================================================================

const AVATAR_SOURCE_STYLES: Record<string, string> = {
  uploaded: 'bg-foreground/5 text-muted-foreground ring-foreground/10',
  discord: 'bg-[#5865F2]/10 text-[#5865F2] ring-[#5865F2]/20',
  default: 'bg-foreground/5 text-muted-foreground/70 ring-foreground/10',
}

function AvatarSourceBadge({ source }: { source?: string }) {
  const { t } = useTranslation()
  const value = source || 'default'
  const labelMap: Record<string, string> = {
    uploaded: t('Uploaded'),
    discord: t('Discord'),
    default: t('Default'),
  }
  const label = labelMap[value] ?? t('Unknown')
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset ${
        AVATAR_SOURCE_STYLES[value] ?? AVATAR_SOURCE_STYLES.default
      }`}
    >
      <span className='text-muted-foreground/80'>{t('Avatar source')}:</span>
      <span>{label}</span>
    </span>
  )
}
