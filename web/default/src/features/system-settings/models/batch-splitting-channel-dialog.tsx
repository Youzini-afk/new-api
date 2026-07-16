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
import { Loader2, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  CHANNEL_STATUS_CONFIG,
  CHANNEL_TYPES,
} from '@/features/channels/constants'
import type { Channel } from '@/features/channels/types'

type BatchSplittingChannelDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channels: Channel[]
  selectedChannelIds: number[]
  onConfirm: (channelIds: number[]) => void
  isLoading?: boolean
  errorMessage?: string
}

export function BatchSplittingChannelDialog({
  open,
  onOpenChange,
  channels,
  selectedChannelIds,
  onConfirm,
  isLoading = false,
  errorMessage = '',
}: BatchSplittingChannelDialogProps) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [draftSelection, setDraftSelection] = useState<Set<number>>(
    new Set(selectedChannelIds)
  )

  useEffect(() => {
    if (!open) return
    setDraftSelection(new Set(selectedChannelIds))
    setSearch('')
  }, [open, selectedChannelIds])

  const filteredChannels = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    if (!keyword) return channels
    return channels.filter((channel) => {
      const typeName =
        CHANNEL_TYPES[channel.type as keyof typeof CHANNEL_TYPES] ?? 'Unknown'
      return (
        channel.id.toString().includes(keyword) ||
        channel.name.toLowerCase().includes(keyword) ||
        (channel.base_url ?? '').toLowerCase().includes(keyword) ||
        typeName.toLowerCase().includes(keyword)
      )
    })
  }, [channels, search])

  const toggleChannel = (channelId: number, checked: boolean) => {
    setDraftSelection((current) => {
      const next = new Set(current)
      if (checked) next.add(channelId)
      else next.delete(channelId)
      return next
    })
  }

  const handleConfirm = () => {
    onConfirm([...draftSelection].sort((a, b) => a - b))
    onOpenChange(false)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Select batching channels')}
      description={t(
        'Only manually selected channel IDs will use request batching. Channel names and Base URLs are never used for automatic matching.'
      )}
      contentClassName='sm:max-w-3xl'
      contentHeight='min(68vh, 640px)'
      bodyClassName='flex h-full min-h-0 flex-col gap-3'
      footer={
        <div className='flex w-full flex-wrap items-center justify-between gap-2'>
          <span className='text-muted-foreground text-xs'>
            {t('{{count}} channels selected', {
              count: draftSelection.size,
            })}
          </span>
          <div className='flex gap-2'>
            <Button
              type='button'
              variant='outline'
              onClick={() => onOpenChange(false)}
            >
              {t('Cancel')}
            </Button>
            <Button type='button' onClick={handleConfirm}>
              {t('Confirm Selection')}
            </Button>
          </div>
        </div>
      }
    >
      <div className='relative'>
        <Search className='text-muted-foreground absolute top-1/2 left-2.5 size-4 -translate-y-1/2' />
        <Input
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={t('Search by channel ID, name, type, or Base URL')}
          className='ps-9'
        />
      </div>

      {isLoading ? (
        <div className='text-muted-foreground flex flex-1 items-center justify-center gap-2 text-sm'>
          <Loader2 className='size-4 animate-spin' />
          {t('Loading channels...')}
        </div>
      ) : null}

      {!isLoading && errorMessage ? (
        <div className='text-destructive flex flex-1 items-center justify-center text-sm'>
          {errorMessage}
        </div>
      ) : null}

      {!isLoading && !errorMessage ? (
        <ScrollArea className='min-h-0 flex-1 rounded-lg border'>
          <div className='divide-y'>
            {filteredChannels.map((channel) => {
              const checkboxId = `batch-splitting-channel-${channel.id}`
              const status =
                CHANNEL_STATUS_CONFIG[
                  channel.status as keyof typeof CHANNEL_STATUS_CONFIG
                ]
              const typeName =
                CHANNEL_TYPES[channel.type as keyof typeof CHANNEL_TYPES] ??
                'Unknown'

              return (
                <div
                  key={channel.id}
                  className='hover:bg-muted/40 flex min-w-0 items-start gap-3 px-3 py-3'
                >
                  <Checkbox
                    id={checkboxId}
                    className='mt-0.5'
                    checked={draftSelection.has(channel.id)}
                    onCheckedChange={(checked) =>
                      toggleChannel(channel.id, checked === true)
                    }
                  />
                  <label
                    htmlFor={checkboxId}
                    className='min-w-0 flex-1 cursor-pointer space-y-1'
                  >
                    <div className='flex min-w-0 flex-wrap items-center gap-2'>
                      <span className='font-medium'>#{channel.id}</span>
                      <span className='min-w-0 truncate font-medium'>
                        {channel.name}
                      </span>
                      <StatusBadge
                        label={t(typeName)}
                        variant='neutral'
                        size='sm'
                        copyable={false}
                      />
                      <StatusBadge
                        label={t(status?.label ?? 'Unknown')}
                        variant={status?.variant ?? 'neutral'}
                        size='sm'
                        copyable={false}
                      />
                    </div>
                    <p
                      className='text-muted-foreground truncate font-mono text-xs'
                      title={channel.base_url ?? ''}
                    >
                      {channel.base_url || t('Default Base URL')}
                    </p>
                  </label>
                </div>
              )
            })}

            {filteredChannels.length === 0 ? (
              <div className='text-muted-foreground px-3 py-12 text-center text-sm'>
                {t('No channels found')}
              </div>
            ) : null}
          </div>
        </ScrollArea>
      ) : null}
    </Dialog>
  )
}
