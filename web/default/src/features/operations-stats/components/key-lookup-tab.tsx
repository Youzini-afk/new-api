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
 * Key Lookup tab for operations stats page.
 */
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { KeyRound, Loader2, Search } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Label } from '@/components/ui/label'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { CopyButton } from '@/components/copy-button'
import { formatQuota } from '@/lib/format'
import { lookupKey } from '../api'
import type { KeyLookupData } from '../types'
import { normalizeLookupKey } from '../lib/utils'

const TOKEN_STATUS_MAP: Record<number, string> = {
  1: 'Enabled',
  2: 'Disabled',
  3: 'Expired',
  4: 'Exhausted',
}

export function KeyLookupTab() {
  const { t } = useTranslation()
  const [inputKey, setInputKey] = useState('')
  const [searchKey, setSearchKey] = useState('')

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['operations-stats', 'key-lookup', searchKey],
    queryFn: async (): Promise<KeyLookupData | null> => {
      if (!searchKey) return null
      const result = await lookupKey(searchKey)
      if (!result.success) {
        throw new Error(result.message || t('Key lookup failed'))
      }
      return result.data ?? null
    },
    enabled: Boolean(searchKey),
    retry: false,
  })

  const handleSearch = () => {
    const normalized = normalizeLookupKey(inputKey)
    if (!normalized) {
      toast.error(t('Please enter an API key'))
      return
    }
    setSearchKey(normalized)
  }

  const tokenStatusLabel = data?.token.status
    ? t(TOKEN_STATUS_MAP[data.token.status] ?? 'Unknown')
    : t('Unknown')

  return (
    <div className='flex h-full flex-col gap-4'>
      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2 text-base'>
            <KeyRound className='size-4' />
            {t('Look up API key owner')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className='flex flex-col gap-3 sm:flex-row sm:items-center'>
            <Input
              value={inputKey}
              onChange={(e) => setInputKey(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleSearch()
              }}
              placeholder={t('Enter an API key (with or without sk- prefix)')}
              className='sm:flex-1'
            />
            <Button
              onClick={handleSearch}
              disabled={isLoading}
              className='gap-1'
            >
              {isLoading ? (
                <Loader2 className='size-4 animate-spin' />
              ) : (
                <Search className='size-4' />
              )}
              {t('Search')}
            </Button>
          </div>
        </CardContent>
      </Card>

      <div className='flex-1'>
        {!searchKey && !isLoading && !data && (
          <EmptyState
            icon={KeyRound}
            title={t('Key Lookup')}
            description={t(
              'Enter an API key to find its owner and token details.'
            )}
            className='min-h-[300px]'
          />
        )}

        {isLoading && (
          <div className='flex min-h-[300px] flex-col items-center justify-center gap-3'>
            <Loader2 className='text-muted-foreground size-8 animate-spin' />
            <p className='text-muted-foreground text-sm'>{t('Searching...')}</p>
          </div>
        )}

        {error && !isLoading && (
          <ErrorState
            title={t('Key lookup failed')}
            description={error.message}
            onRetry={refetch}
            className='min-h-[300px]'
          />
        )}

        {data && !isLoading && (
          <div className='grid gap-4 md:grid-cols-2'>
            <Card>
              <CardHeader>
                <CardTitle className='text-base'>{t('User')}</CardTitle>
              </CardHeader>
              <CardContent className='space-y-3'>
                <InfoRow label={t('Username')} value={data.user.username} />
                {data.user.display_name && (
                  <InfoRow
                    label={t('Display Name')}
                    value={data.user.display_name}
                  />
                )}
                <InfoRow label={t('User ID')} value={data.user.id} />
                {data.user.group && (
                  <InfoRow label={t('Group')} value={data.user.group} />
                )}
                {(data.user.quota !== undefined ||
                  data.user.used_quota !== undefined) && (
                  <div className='grid grid-cols-2 gap-3 pt-2 border-t'>
                    {data.user.quota !== undefined && (
                      <InfoRow
                        label={t('Balance')}
                        value={formatQuota(data.user.quota)}
                      />
                    )}
                    {data.user.used_quota !== undefined && (
                      <InfoRow
                        label={t('Used Quota')}
                        value={formatQuota(data.user.used_quota)}
                      />
                    )}
                  </div>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className='text-base'>{t('Token')}</CardTitle>
              </CardHeader>
              <CardContent className='space-y-3'>
                <InfoRow label={t('Name')} value={data.token.name || '-'} />
                <InfoRow
                  label={t('Status')}
                  value={
                    <Badge variant='outline'>{tokenStatusLabel}</Badge>
                  }
                />
                <InfoRow
                  label={t('Masked Key')}
                  value={
                    <div className='flex items-center gap-2'>
                      <span className='font-mono text-xs'>
                        {data.key_masked || data.token.key}
                      </span>
                      <CopyButton
                        value={data.key_masked || data.token.key}
                        size='icon'
                        className='size-6'
                        variant='ghost'
                        tooltip={t('Copy masked key')}
                      />
                    </div>
                  }
                />
                <div className='grid grid-cols-2 gap-3 pt-2 border-t'>
                  <InfoRow
                    label={t('Remaining Quota')}
                    value={formatQuota(data.token.remain_quota)}
                  />
                  <InfoRow
                    label={t('Used Quota')}
                    value={formatQuota(data.token.used_quota)}
                  />
                </div>
                {data.token.group && (
                  <InfoRow label={t('Group')} value={data.token.group} />
                )}
              </CardContent>
            </Card>
          </div>
        )}
      </div>
    </div>
  )
}

function InfoRow({
  label,
  value,
}: {
  label: string
  value: React.ReactNode
}) {
  return (
    <div className='flex flex-col gap-0.5'>
      <Label className='text-muted-foreground text-xs'>{label}</Label>
      <div className='text-sm font-medium'>{value}</div>
    </div>
  )
}
