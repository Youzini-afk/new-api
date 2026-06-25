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
  AlertTriangle,
  CheckCircle2,
  Hash,
  Radio,
  Tag,
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
  tone: 'neutral' | 'good' | 'warn' | 'bad'
}

const TONE_STYLES: Record<CardConfig['tone'], string> = {
  neutral: 'text-muted-foreground bg-muted/40',
  good: 'text-emerald-600 bg-emerald-500/10 dark:text-emerald-400',
  warn: 'text-amber-600 bg-amber-500/10 dark:text-amber-400',
  bad: 'text-rose-600 bg-rose-500/10 dark:text-rose-400',
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
          key: 'total',
          labelKey: 'Total Errors',
          icon: AlertTriangle,
          value: formatCompactNumber(data.total_count),
          tone: 'neutral',
        },
        {
          key: 'matched',
          labelKey: 'Rule Matched',
          icon: CheckCircle2,
          value: formatCompactNumber(data.rule_matched_count),
          tone: 'good',
        },
        {
          key: 'unmatched',
          labelKey: 'Unmatched',
          icon: XCircle,
          value: formatCompactNumber(data.unmatched_count),
          tone: 'bad',
        },
        {
          key: 'signatures',
          labelKey: 'Distinct Signatures',
          icon: Hash,
          value: formatCompactNumber(data.distinct_signatures),
          tone: 'neutral',
        },
        {
          key: 'users',
          labelKey: 'Affected Users',
          icon: Users,
          value: formatCompactNumber(data.affected_users),
          tone: 'warn',
        },
        {
          key: 'channels',
          labelKey: 'Affected Channels',
          icon: Radio,
          value: formatCompactNumber(data.affected_channels),
          tone: 'warn',
        },
        {
          key: 'top-rule',
          labelKey: 'Top Rule',
          icon: Tag,
          value: data.top_rule_code || '-',
          hintKey: 'Count',
          hintValue: data.top_rule_code
            ? formatCompactNumber(data.top_rule_code_count)
            : '-',
          tone: 'neutral',
        },
      ]
    : []

  return (
    <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-7'>
      {isLoading
        ? Array.from({ length: 7 }).map((_, index) => (
            <Card key={index} className='overflow-hidden'>
              <CardContent className='flex flex-col gap-2 p-3.5'>
                <Skeleton className='h-3 w-20' />
                <Skeleton className='h-6 w-16' />
              </CardContent>
            </Card>
          ))
        : cards.map((card) => {
            const Icon = card.icon
            return (
              <Card key={card.key} className='overflow-hidden'>
                <CardContent className='flex flex-col gap-2 p-3.5'>
                  <div className='flex items-center gap-2'>
                    <span
                      className={cn(
                        'flex size-7 items-center justify-center rounded-lg',
                        TONE_STYLES[card.tone]
                      )}
                    >
                      <Icon className='size-4' />
                    </span>
                    <span className='text-muted-foreground truncate text-xs font-medium'>
                      {t(card.labelKey)}
                    </span>
                  </div>
                  <div className='flex items-baseline gap-2'>
                    <span className='truncate text-xl font-semibold tabular-nums'>
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
