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
 * Summary metric cards for the error insight page.
 */
import { useTranslation } from 'react-i18next'
import {
  CheckCircle2,
  Hash,
  Radio,
  Users,
  XCircle,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { ErrorState } from '@/components/error-state'
import { formatCompactNumber } from '@/lib/format'
import type { ErrorInsightSummary } from '../types'

interface SummaryCardsProps {
  data?: ErrorInsightSummary
  isLoading: boolean
  error: Error | null
  onRetry?: () => void
}

interface CardConfig {
  key: string
  labelKey: string
  icon: React.ComponentType<{ className?: string }>
  value: string
  hintKey?: string
  hintValue?: string
  tone: 'peach' | 'amber' | 'mint' | 'neutral'
}

const TONE_STYLES: Record<CardConfig['tone'], string> = {
  peach: 'bg-orange-100 text-orange-500 dark:bg-orange-100 dark:text-orange-500',
  amber: 'bg-amber-100 text-amber-500 dark:bg-amber-100 dark:text-amber-500',
  mint: 'bg-cyan-100 text-cyan-500 dark:bg-cyan-100 dark:text-cyan-500',
  neutral: 'bg-muted/60 text-muted-foreground',
}

export function SummaryCards(props: SummaryCardsProps) {
  const { t } = useTranslation()
  const { data, isLoading, error, onRetry } = props

  if (error) {
    return (
      <ErrorState
        title={t('Failed to load summary')}
        description={error.message}
        onRetry={onRetry}
        className='min-h-[120px]'
      />
    )
  }

  const cards: CardConfig[] = data
    ? [
          {
            key: 'unmatched',
            labelKey: 'Unmatched Errors',
            icon: XCircle,
            value: formatCompactNumber(data.unmatched_count),
            tone: 'amber',
          },
          {
            key: 'matched',
            labelKey: 'Matched Errors',
            icon: CheckCircle2,
            value: formatCompactNumber(data.rule_matched_count),
            tone: 'mint',
          },
          {
            key: 'signatures',
            labelKey: 'Error Signatures',
            icon: Hash,
            value: formatCompactNumber(data.distinct_signatures),
            tone: 'neutral',
          },
          {
            key: 'users',
            labelKey: 'Affected Users',
            icon: Users,
            value: formatCompactNumber(data.affected_users),
            tone: 'neutral',
          },
          {
            key: 'channels',
            labelKey: 'Affected Channels',
            icon: Radio,
            value: formatCompactNumber(data.affected_channels),
            tone: 'neutral',
          },
        ]
    : []

  return (
    <div className='grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3'>
      {isLoading
        ? Array.from({ length: 5 }).map((_, index) => (
            <Card
              key={index}
              className='bg-card/90 overflow-hidden border-0 shadow-sm'
            >
              <CardContent className='flex min-h-[110px] flex-col justify-center gap-3 p-6'>
                <Skeleton className='h-4 w-24' />
                <Skeleton className='h-8 w-20' />
              </CardContent>
            </Card>
          ))
        : cards.map((card) => {
            const Icon = card.icon
            return (
              <Card
                key={card.key}
                className='bg-card/90 overflow-hidden border-0 shadow-sm'
              >
                <CardContent className='flex min-h-[110px] items-center gap-5 p-6'>
                  {['peach', 'amber', 'mint'].includes(card.tone) && (
                    <span
                      className={cn(
                        'flex size-14 shrink-0 items-center justify-center rounded-xl',
                        TONE_STYLES[card.tone]
                      )}
                    >
                      <Icon className='size-6' />
                    </span>
                  )}
                  <div className='flex min-w-0 flex-col gap-2'>
                    <span className='text-primary/70 truncate text-sm font-semibold'>
                      {t(card.labelKey)}
                    </span>
                    <span className='truncate text-3xl font-bold tracking-tight tabular-nums'>
                      {card.value}
                    </span>
                    {card.hintKey && (
                      <span className='text-muted-foreground truncate text-xs'>
                        {t(card.hintKey)}: {card.hintValue}
                      </span>
                    )}
                  </div>
                </CardContent>
              </Card>
            )
          })}
    </div>
  )
}
