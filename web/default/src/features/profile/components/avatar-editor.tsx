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
import { useCallback, useMemo, useRef, useState } from 'react'
import { Camera, Loader2, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import {
  getSafeUserAvatarUrl,
  getUserAvatarFallback,
  getUserAvatarStyle,
} from '@/lib/avatar'
import {
  Avatar,
  AvatarFallback,
  AvatarImage,
} from '@/components/ui/avatar'
import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  AVATAR_ACCEPT,
  AvatarValidationError,
  compressAvatarImage,
} from '../lib/avatar-image'
import { deleteUserAvatar, uploadUserAvatar } from '../api'
import type { UserProfile } from '../types'

// ============================================================================
// Avatar Editor Component
// ============================================================================
// Renders the user's avatar (uploaded image when present, colored initials
// fallback otherwise) with hover controls to change or remove it. Handles
// client-side compression, the multipart upload, the auth-store sync so the
// header/dropdown/drawer update instantly, and a refresh of the profile data.
// ============================================================================

interface AvatarEditorProps {
  profile: UserProfile
  onProfileUpdate: () => void
}

export function AvatarEditor({ profile, onProfileUpdate }: AvatarEditorProps) {
  const { t } = useTranslation()
  const setUser = useAuthStore((state) => state.auth.setUser)

  const inputRef = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [confirmRemove, setConfirmRemove] = useState(false)

  const avatarName = profile.username || profile.display_name || ''
  const avatarFallback = getUserAvatarFallback(avatarName)
  const avatarFallbackStyle = useMemo(
    () => getUserAvatarStyle(avatarName),
    [avatarName]
  )
  const safeAvatarUrl = getSafeUserAvatarUrl(profile.avatar_url)
  const canRemoveAvatar =
    profile.avatar_source === 'uploaded' && Boolean(profile.avatar_url)

  const syncAuthUser = useCallback(
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

  const handleFile = useCallback(
    async (file: File | undefined) => {
      if (!file) return
      try {
        setUploading(true)
        const blob = await compressAvatarImage(file)
        const response = await uploadUserAvatar(blob)
        if (response.success) {
          const next = response.data ?? {}
          toast.success(t('Avatar updated successfully'))
          syncAuthUser({
            avatar_url: next.avatar_url ?? '',
            avatar_source: next.avatar_source ?? '',
          })
          await onProfileUpdate()
        } else {
          toast.error(response.message || t('Failed to update avatar'))
        }
      } catch (error) {
        if (error instanceof AvatarValidationError) {
          toast.error(error.message)
        } else {
          // eslint-disable-next-line no-console
          console.error('Failed to upload avatar:', error)
          toast.error(t('Failed to update avatar'))
        }
      } finally {
        setUploading(false)
        if (inputRef.current) inputRef.current.value = ''
      }
    },
    [onProfileUpdate, syncAuthUser, t]
  )

  const handleRemove = useCallback(async () => {
    setConfirmRemove(false)
    try {
      setRemoving(true)
      const response = await deleteUserAvatar()
      if (response.success) {
        const next = response.data ?? {}
        toast.success(t('Avatar removed'))
        syncAuthUser({
          avatar_url: next.avatar_url ?? '',
          avatar_source: next.avatar_source ?? '',
        })
        await onProfileUpdate()
      } else {
        toast.error(response.message || t('Failed to remove avatar'))
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to remove avatar:', error)
      toast.error(t('Failed to remove avatar'))
    } finally {
      setRemoving(false)
    }
  }, [onProfileUpdate, syncAuthUser, t])

  return (
    <div className='group/avatar-edit relative inline-flex shrink-0'>
      <Avatar className='ring-background h-12 w-12 text-sm ring-2 sm:h-16 sm:w-16 sm:text-lg sm:ring-4'>
        {safeAvatarUrl && (
          <AvatarImage
            src={safeAvatarUrl}
            alt={t('Avatar of {{name}}', { name: avatarName })}
          />
        )}
        <AvatarFallback
          className='font-semibold text-white'
          style={avatarFallbackStyle}
        >
          {avatarFallback}
        </AvatarFallback>
      </Avatar>

      {/* Change overlay — pencil/camera button, hover-revealed */}
      <button
        type='button'
        onClick={() => inputRef.current?.click()}
        disabled={uploading || removing}
        aria-label={t('Change avatar')}
        className='bg-foreground/60 hover:bg-foreground/80 absolute inset-0 flex items-center justify-center rounded-full text-white opacity-0 transition-opacity duration-150 group-hover/avatar-edit:opacity-100 focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-offset-2 disabled:cursor-not-allowed'
      >
        {uploading ? (
          <Loader2 className='h-4 w-4 animate-spin sm:h-5 sm:w-5' />
        ) : (
          <Camera className='h-4 w-4 sm:h-5 sm:w-5' />
        )}
      </button>

      {/* Remove control — only when an uploaded avatar exists */}
      {canRemoveAvatar && !uploading && (
        <button
          type='button'
          onClick={() => setConfirmRemove(true)}
          disabled={removing}
          aria-label={t('Remove avatar')}
          className='bg-destructive hover:bg-destructive/90 absolute -right-1 -bottom-1 flex size-5 items-center justify-center rounded-full text-white shadow-sm transition-colors disabled:cursor-not-allowed sm:size-6'
        >
          {removing ? (
            <Loader2 className='h-3 w-3 animate-spin' />
          ) : (
            <Trash2 className='h-3 w-3' />
          )}
        </button>
      )}

      <input
        ref={inputRef}
        type='file'
        accept={AVATAR_ACCEPT}
        className='sr-only'
        onChange={(event) => {
          const file = event.target.files?.[0]
          event.target.value = ''
          void handleFile(file)
        }}
      />

      <ConfirmDialog
        open={confirmRemove}
        onOpenChange={setConfirmRemove}
        title={t('Remove avatar')}
        desc={t(
          'Remove your uploaded avatar? You will revert to the default avatar.'
        )}
        confirmText={t('Remove')}
        destructive
        handleConfirm={() => void handleRemove()}
        isLoading={removing}
      />
    </div>
  )
}
