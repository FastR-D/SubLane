import { TimeSeriesChart } from './TimeSeriesChart'
import { useTranslation } from 'react-i18next'
import {
  metricLabels,
  metricValue,
  metricPartial,
  type Statistic,
  type UsageMetric,
} from '@/lib/statistics'

const value = metricValue
const partial = metricPartial

export function UsageChart({
  rows,
  metric,
  daily,
  label,
  timeZone = 'UTC',
}: {
  rows: Statistic[]
  metric: UsageMetric
  daily: boolean
  label: string
  timeZone?: string
}) {
  const { t, i18n } = useTranslation()
  const numbers = new Intl.NumberFormat(i18n.resolvedLanguage ?? 'en')
  if (!rows.length)
    return (
      <p className="py-12 text-center text-sm text-muted-foreground">
        {t('noUsageSummary')}
      </p>
    )
  const amounts = rows.map((row) => value(row, metric))
  if (
    metric !== 'requests' &&
    !rows.some(
      (row) =>
        (metric === 'input_tokens' ? row.input_reported : row.output_reported) >
        0,
    ) &&
    rows.some((row) => row.requests > 0)
  )
    return (
      <p className="py-12 text-center text-sm text-muted-foreground">
        {t('usageNoTokenReports')}
      </p>
    )
  if (daily)
    return (
      <TimeSeriesChart
        rows={rows}
        valueLabel={t(metricLabels[metric])}
        partialValues={rows.map((row) => partial(row, metric))}
        amounts={amounts}
        label={label}
        timeZone={timeZone}
      />
    )
  const maximum = Math.max(1, ...amounts.map((amount) => amount ?? 0))
  return (
    <div className="space-y-5">
      <ol
        aria-label={label}
        className="max-h-[28rem] space-y-5 overflow-y-auto pr-2"
      >
        {rows.map((row, index) => (
          <li key={row.id} className="space-y-2">
            <div className="flex items-start justify-between gap-4 text-sm">
              <span
                className="min-w-0 break-words font-medium"
                title={row.name}
              >
                {row.name}
              </span>
              <span className="shrink-0 tabular-nums">
                {amounts[index] === null
                  ? '—'
                  : numbers.format(amounts[index]!)}
              </span>
            </div>
            <div
              aria-hidden="true"
              className="h-2 overflow-hidden rounded-full bg-muted"
            >
              <div
                className="h-full rounded-full bg-foreground"
                style={{ width: ((amounts[index] ?? 0) / maximum) * 100 + '%' }}
              />
            </div>
            {partial(row, metric) && (
              <p className="text-xs text-muted-foreground">
                {t(
                  amounts[index] === null
                    ? 'usageTokenNotReported'
                    : 'usageReportedOnly',
                )}
              </p>
            )}
          </li>
        ))}
      </ol>
      <p className="text-xs text-muted-foreground">{t('usageRankHint')}</p>
    </div>
  )
}
