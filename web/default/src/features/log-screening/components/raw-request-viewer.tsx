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
 * Collapsible viewer for the raw request headers / params captured on an
 * interception. The backend masks and truncates these before persisting, so
 * the UI surfaces them collapsed by default with an explicit notice.
 */
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight, ShieldAlert } from 'lucide-react'
import { CopyButton } from '@/components/copy-button'
import { cn } from '@/lib/utils'

interface RawRequestViewerProps {
  headersRaw: string
  paramsRaw: string
  className?: string
}

export function RawRequestViewer(props: RawRequestViewerProps) {
  const { t } = useTranslation()
  const [openHeaders, setOpenHeaders] = useState(false)
  const [openParams, setOpenParams] = useState(false)

  const headers = (props.headersRaw || '').trim()
  const params = (props.paramsRaw || '').trim()

  return (
    <div className={cn('flex flex-col gap-2', props.className)}>
      <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
        <ShieldAlert className='size-3.5' />
        {t('Raw request data is masked and truncated by the backend.')}
      </div>

      <RawBlock
        label={t('Request Headers')}
        raw={headers}
        open={openHeaders}
        onOpenChange={setOpenHeaders}
      />
      <RawBlock
        label={t('Request Params')}
        raw={params}
        open={openParams}
        onOpenChange={setOpenParams}
      />
    </div>
  )
}

interface RawBlockProps {
  label: string
  raw: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

function RawBlock(props: RawBlockProps) {
  const { t } = useTranslation()
  const isEmpty = props.raw === ''

  return (
    <div className='bg-muted/40 rounded-lg border'>
      <button
        type='button'
        className='hover:bg-muted/60 flex w-full items-center justify-between gap-2 rounded-lg px-3 py-2 text-left'
        onClick={() => props.onOpenChange(!props.open)}
        aria-expanded={props.open}
      >
        <span className='flex items-center gap-1.5 text-sm font-medium'>
          {props.open ? (
            <ChevronDown className='size-3.5' />
          ) : (
            <ChevronRight className='size-3.5' />
          )}
          {props.label}
        </span>
        {!isEmpty && (
          <CopyButton
            value={props.raw}
            size='icon'
            variant='ghost'
            tooltip={t('Copy')}
          />
        )}
      </button>
      {props.open && (
        <pre className='text-muted-foreground max-h-[280px] overflow-auto rounded-b-lg px-3 pb-3 font-mono text-xs whitespace-pre-wrap break-all'>
          {isEmpty ? t('No data') : props.raw}
        </pre>
      )}
    </div>
  )
}
