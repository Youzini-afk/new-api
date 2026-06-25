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
 * Filter bar for the error insight page.
 *
 * Drives every downstream query: a relative time range preset, a
 * matched/unmatched toggle, plus free-text filters for rule code, unmatched
 * reason and model name.
 */
import { useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Search } from 'lucide-react'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  RULE_MATCHED_OPTIONS,
  TIME_RANGE_PRESETS,
  type RuleMatchedFilter,
  type TimeRangePreset,
} from '../constants'

export interface FiltersValue {
  timeRange: TimeRangePreset
  ruleMatched: RuleMatchedFilter
  ruleCode: string
  unmatchedReason: string
  modelName: string
}

interface FiltersBarProps {
  value: FiltersValue
  onChange: (value: FiltersValue) => void
}

export function FiltersBar(props: FiltersBarProps) {
  const { t } = useTranslation()
  const { value, onChange } = props

  const patch = useCallback(
    (partial: Partial<FiltersValue>) => {
      onChange({ ...value, ...partial })
    },
    [value, onChange]
  )

  return (
    <div className='bg-card ring-foreground/10 flex flex-wrap items-center gap-2 rounded-xl p-2.5 ring-1'>
      <Select
        value={String(value.timeRange)}
        onValueChange={(v) =>
          patch({ timeRange: Number(v) as TimeRangePreset })
        }
      >
        <SelectTrigger className='w-[140px]'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {TIME_RANGE_PRESETS.map((preset) => (
            <SelectItem key={preset.value} value={String(preset.value)}>
              {t(preset.labelKey)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select
        value={value.ruleMatched}
        onValueChange={(v) =>
          patch({ ruleMatched: v as RuleMatchedFilter })
        }
      >
        <SelectTrigger className='w-[140px]'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {RULE_MATCHED_OPTIONS.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {t(option.labelKey)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <div className='relative'>
        <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2' />
        <Input
          value={value.ruleCode}
          onChange={(e) => patch({ ruleCode: e.target.value })}
          placeholder={t('Rule code')}
          className='w-[180px] pl-8'
        />
      </div>

      <div className='relative'>
        <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2' />
        <Input
          value={value.unmatchedReason}
          onChange={(e) => patch({ unmatchedReason: e.target.value })}
          placeholder={t('Unmatched reason')}
          className='w-[200px] pl-8'
        />
      </div>

      <div className='relative'>
        <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2' />
        <Input
          value={value.modelName}
          onChange={(e) => patch({ modelName: e.target.value })}
          placeholder={t('Model name')}
          className='w-[200px] pl-8'
        />
      </div>
    </div>
  )
}
