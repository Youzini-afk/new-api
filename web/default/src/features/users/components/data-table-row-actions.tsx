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
import { type Row } from '@tanstack/react-table'
import {
  MoreHorizontal,
  Pencil,
  Trash2,
  Power,
  PowerOff,
  ArrowUp,
  ArrowDown,
  KeyRound,
  ShieldAlert,
  Link2,
  CreditCard,
  RotateCcw,
  UserX,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { UserSubscriptionsDialog } from '@/features/subscriptions/components/dialogs/user-subscriptions-dialog'
import {
  forceUserDiscordGateReauth,
  manageUser,
  recheckUserDiscordGate,
  resetUserPasskey,
  resetUserTwoFA,
  setUserDiscordGateExempt,
} from '../api'
import {
  USER_STATUS,
  USER_ROLE,
  ERROR_MESSAGES,
  isUserDeleted,
} from '../constants'
import { getUserActionMessage } from '../lib'
import { type User, type ManageUserAction } from '../types'
import { UserBindingDialog } from './dialogs/user-binding-dialog'
import { useUsers } from './users-provider'

interface DataTableRowActionsProps {
  row: Row<User>
}

export function DataTableRowActions({ row }: DataTableRowActionsProps) {
  const { t } = useTranslation()
  const user = row.original
  const { setOpen, setCurrentRow, triggerRefresh } = useUsers()
  const [resetPasskeyOpen, setResetPasskeyOpen] = useState(false)
  const [resetTwoFAOpen, setResetTwoFAOpen] = useState(false)
  const [forceDiscordReauthOpen, setForceDiscordReauthOpen] = useState(false)
  const [bindingDialogOpen, setBindingDialogOpen] = useState(false)
  const [subscriptionsDialogOpen, setSubscriptionsDialogOpen] = useState(false)

  const handleEdit = () => {
    setCurrentRow(user)
    setOpen('update')
  }

  const handleDelete = () => {
    setCurrentRow(user)
    setOpen('delete')
  }

  const handleManage = async (action: Exclude<ManageUserAction, 'delete'>) => {
    try {
      const result = await manageUser(user.id, action)
      if (result.success) {
        toast.success(t(getUserActionMessage(action)))
        triggerRefresh()
      } else {
        toast.error(
          result.message || t('Failed to {{action}} user', { action })
        )
      }
    } catch (_error) {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    }
  }

  const handleResetPasskey = async () => {
    try {
      const result = await resetUserPasskey(user.id)
      if (result.success) {
        toast.success(t('Passkey reset successfully'))
        triggerRefresh()
      } else {
        toast.error(result.message || t('Failed to reset Passkey'))
      }
    } catch (_error) {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setResetPasskeyOpen(false)
    }
  }

  const handleResetTwoFA = async () => {
    try {
      const result = await resetUserTwoFA(user.id)
      if (result.success) {
        toast.success(t('Two-factor authentication reset'))
        triggerRefresh()
      } else {
        toast.error(result.message || t('Failed to reset 2FA'))
      }
    } catch (_error) {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setResetTwoFAOpen(false)
    }
  }

  const handleDiscordRecheck = async () => {
    try {
      const result = await recheckUserDiscordGate(user.id)
      if (result.success) {
        toast.success(t('Discord gate rechecked'))
        triggerRefresh()
      } else {
        toast.error(result.message || t('Failed to recheck Discord gate'))
      }
    } catch (_error) {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    }
  }

  const handleForceDiscordReauth = async () => {
    try {
      const result = await forceUserDiscordGateReauth(user.id)
      if (result.success) {
        toast.success(t('Discord reauthorization required'))
        triggerRefresh()
      } else {
        toast.error(result.message || t('Failed to force Discord reauth'))
      }
    } catch (_error) {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setForceDiscordReauthOpen(false)
    }
  }

  const handleDiscordExempt = async () => {
    const nextExempt = !user.discord_gate_exempt
    try {
      const result = await setUserDiscordGateExempt(user.id, nextExempt)
      if (result.success) {
        toast.success(
          nextExempt
            ? t('Discord exemption enabled')
            : t('Discord exemption disabled')
        )
        triggerRefresh()
      } else {
        toast.error(result.message || t('Failed to update Discord exemption'))
      }
    } catch (_error) {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    }
  }

  const isDisabled = user.status === USER_STATUS.DISABLED
  const isAdmin = user.role >= USER_ROLE.ADMIN
  const isRoot = user.role === USER_ROLE.ROOT

  if (isUserDeleted(user)) {
    return null
  }

  return (
    <div className='-ml-2'>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant='ghost'
              className='data-popup-open:bg-muted flex h-8 w-8 p-0'
            />
          }
        >
          <MoreHorizontal className='h-4 w-4' />
          <span className='sr-only'>{t('Open menu')}</span>
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end' className='w-[180px]'>
          <DropdownMenuItem onClick={handleEdit}>
            {t('Edit')}
            <DropdownMenuShortcut>
              <Pencil size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuSeparator />

          {isDisabled ? (
            <DropdownMenuItem onClick={() => handleManage('enable')}>
              {t('Enable')}
              <DropdownMenuShortcut>
                <Power size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem
              onClick={() => handleManage('disable')}
              disabled={isRoot}
            >
              {t('Disable')}
              <DropdownMenuShortcut>
                <PowerOff size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          {isAdmin && !isRoot && (
            <DropdownMenuItem onClick={() => handleManage('demote')}>
              {t('Demote')}
              <DropdownMenuShortcut>
                <ArrowDown size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          {!isAdmin && (
            <DropdownMenuItem onClick={() => handleManage('promote')}>
              {t('Promote')}
              <DropdownMenuShortcut>
                <ArrowUp size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              setBindingDialogOpen(true)
            }}
          >
            {t('Manage Bindings')}
            <DropdownMenuShortcut>
              <Link2 size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              setSubscriptionsDialogOpen(true)
            }}
          >
            {t('Manage Subscriptions')}
            <DropdownMenuShortcut>
              <CreditCard size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuSeparator />

          <DropdownMenuItem onClick={handleDiscordRecheck} disabled={isRoot}>
            {t('Recheck Discord gate')}
            <DropdownMenuShortcut>
              <RotateCcw size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              setForceDiscordReauthOpen(true)
            }}
            disabled={isRoot}
          >
            {t('Force Discord reauth')}
            <DropdownMenuShortcut>
              <UserX size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuItem onClick={handleDiscordExempt} disabled={isRoot}>
            {user.discord_gate_exempt
              ? t('Unmark Discord exempt')
              : t('Mark Discord exempt')}
            <DropdownMenuShortcut>
              <ShieldCheck size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuSeparator />

          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              setResetPasskeyOpen(true)
            }}
            disabled={isRoot}
          >
            {t('Reset Passkey')}
            <DropdownMenuShortcut>
              <KeyRound size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              setResetTwoFAOpen(true)
            }}
            disabled={isRoot}
          >
            {t('Reset 2FA')}
            <DropdownMenuShortcut>
              <ShieldAlert size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuSeparator />

          <DropdownMenuItem
            onClick={handleDelete}
            className='text-destructive focus:text-destructive'
            disabled={isRoot}
          >
            {t('Delete')}
            <DropdownMenuShortcut>
              <Trash2 size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <ConfirmDialog
        open={resetPasskeyOpen}
        onOpenChange={setResetPasskeyOpen}
        title={t('Reset Passkey')}
        desc={t(
          'Reset Passkey for {{username}}? The user will need to register a new Passkey before using passwordless login.',
          { username: user.username }
        )}
        confirmText={t('Reset Passkey')}
        handleConfirm={handleResetPasskey}
      />

      <ConfirmDialog
        open={resetTwoFAOpen}
        onOpenChange={setResetTwoFAOpen}
        title={t('Reset Two-Factor Authentication')}
        desc={t(
          'Reset 2FA for {{username}}? The user must set up 2FA again to continue using it.',
          { username: user.username }
        )}
        confirmText={t('Reset 2FA')}
        handleConfirm={handleResetTwoFA}
      />

      <ConfirmDialog
        open={forceDiscordReauthOpen}
        onOpenChange={setForceDiscordReauthOpen}
        title={t('Force Discord reauth')}
        desc={t(
          'Force Discord reauthorization for {{username}}? The user must reconnect Discord before passing the gate again.',
          { username: user.username }
        )}
        confirmText={t('Force Discord reauth')}
        handleConfirm={handleForceDiscordReauth}
      />

      <UserBindingDialog
        open={bindingDialogOpen}
        onOpenChange={setBindingDialogOpen}
        userId={user.id}
        onUnbindSuccess={triggerRefresh}
      />

      <UserSubscriptionsDialog
        open={subscriptionsDialogOpen}
        onOpenChange={setSubscriptionsDialogOpen}
        user={{ id: user.id, username: user.username }}
        onSuccess={triggerRefresh}
      />
    </div>
  )
}
