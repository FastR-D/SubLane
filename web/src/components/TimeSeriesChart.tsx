import {
  useEffect,
  useId,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent,
} from 'react'
import { useTranslation } from 'react-i18next'

export function TimeSeriesChart({
  rows,
  valueLabel,
  partialValues,
  timeDetail = false,
  missingLabel,
  amounts,
  label,
  timeZone,
}: {
  rows: { id: string }[]
  valueLabel: string
  partialValues?: boolean[]
  timeDetail?: boolean
  missingLabel?: string
  amounts: (number | null)[]
  label: string
  timeZone: string
}) {
  const { t, i18n } = useTranslation()
  const help = useId()
  const container = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(720)
  const [active, setActive] = useState<number | null>(null)
  useEffect(() => {
    const element = container.current
    if (!element) return
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver((entries) => {
      const next = entries[0]?.contentRect.width
      if (next) setWidth(Math.max(240, Math.round(next)))
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const locale = i18n.resolvedLanguage ?? 'en'
  const numbers = new Intl.NumberFormat(locale)
  const compact = new Intl.NumberFormat(locale, {
    notation: 'compact',
    maximumFractionDigits: 1,
  })
  const dates = new Intl.DateTimeFormat(locale, {
    month: 'short',
    day: 'numeric',
    hour: timeDetail ? '2-digit' : undefined,
    minute: timeDetail ? '2-digit' : undefined,
    timeZone,
  })
  const fullDates = new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: timeDetail ? 'short' : undefined,
    timeZone,
  })
  const maximum = Math.max(1, ...amounts.map((amount) => amount ?? 0))
  const magnitude = 10 ** Math.floor(Math.log10(maximum / 4))
  const step = Math.max(
    1,
    ([1, 2, 5, 10].find((size) => size * magnitude >= maximum / 4) ?? 10) *
      magnitude,
  )
  const top = Math.ceil(maximum / step) * step
  const left = 52,
    right = 16,
    height = 260,
    bottom = 222
  const x = (index: number) =>
    rows.length === 1
      ? (left + width - right) / 2
      : left +
        (timeDetail
          ? (Number(rows[index].id) - Number(rows[0].id)) /
            Math.max(1, Number(rows[rows.length - 1].id) - Number(rows[0].id))
          : index / (rows.length - 1)) *
          (width - left - right)
  const y = (amount: number) => bottom - (amount / top) * (bottom - 16)
  const ticks = Array.from(
    { length: Math.round(top / step) + 1 },
    (_, index) => index * step,
  )
  const tickCount = Math.min(
    rows.length,
    timeDetail ? (width < 480 ? 2 : 4) : width < 480 ? 3 : 6,
  )
  const datesToShow = new Set(
    Array.from({ length: tickCount }, (_, index) =>
      tickCount === 1
        ? 0
        : Math.round((index * (rows.length - 1)) / (tickCount - 1)),
    ),
  )
  const path = amounts
    .map((amount, index) => {
      if (amount === null) return ''
      // Restart after unknown samples; connecting across them would imply observed usage.
      const command = index === 0 || amounts[index - 1] === null ? 'M' : 'L'
      return command + x(index) + ',' + y(amount)
    })
    .join(' ')
  const select = (event: PointerEvent<SVGSVGElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    const relative = ((event.clientX - bounds.left) * width) / bounds.width
    if (timeDetail) {
      let nearest = 0
      for (let i = 1; i < rows.length; i++) {
        if (Math.abs(x(i) - relative) < Math.abs(x(nearest) - relative))
          nearest = i
      }
      setActive(nearest)
      return
    }
    setActive(
      Math.max(
        0,
        Math.min(
          rows.length - 1,
          Math.round(
            ((relative - left) / (width - left - right)) * (rows.length - 1),
          ),
        ),
      ),
    )
  }
  const navigate = (event: KeyboardEvent<SVGSVGElement>) => {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    setActive((current) =>
      event.key === 'Home'
        ? 0
        : event.key === 'End'
          ? rows.length - 1
          : Math.max(
              0,
              Math.min(
                rows.length - 1,
                (current ?? 0) + (event.key === 'ArrowLeft' ? -1 : 1),
              ),
            ),
    )
  }
  const selected = active === null ? null : rows[active]
  return (
    <div ref={container} className="space-y-3">
      <svg
        role="img"
        aria-label={label}
        aria-describedby={help}
        tabIndex={0}
        viewBox={'0 0 ' + width + ' ' + height}
        height={height}
        className="w-full rounded-sm outline-none focus-visible:outline-2 focus-visible:outline-ring"
        onPointerMove={select}
        onPointerDown={select}
        onPointerLeave={() => setActive(null)}
        onFocus={() => setActive(rows.length - 1)}
        onBlur={() => setActive(null)}
        onKeyDown={navigate}
      >
        {ticks.map((tick) => (
          <g key={tick}>
            <line
              x1={left}
              x2={width - right}
              y1={y(tick)}
              y2={y(tick)}
              stroke="var(--border)"
              strokeDasharray={tick === 0 ? undefined : '3 4'}
            />
            <text
              x={left - 10}
              y={y(tick) + 4}
              textAnchor="end"
              fontSize={12}
              fill="var(--muted-foreground)"
            >
              {compact.format(tick)}
            </text>
          </g>
        ))}
        <path
          d={path}
          fill="none"
          stroke="var(--foreground)"
          strokeWidth={2}
          vectorEffect="non-scaling-stroke"
        />
        {amounts.map((amount, index) =>
          amount !== null && (rows.length <= 14 || index === active) ? (
            <circle
              key={rows[index].id}
              cx={x(index)}
              cy={y(amount)}
              r={index === active ? 5 : 3}
              fill="var(--background)"
              stroke="var(--foreground)"
              strokeWidth={2}
            />
          ) : null,
        )}
        {active !== null && (
          <line
            x1={x(active)}
            x2={x(active)}
            y1={16}
            y2={bottom}
            stroke="var(--muted-foreground)"
            strokeDasharray="3 4"
          />
        )}
        {rows.map((row, index) =>
          datesToShow.has(index) ? (
            <text
              key={row.id}
              x={x(index)}
              y={height - 12}
              textAnchor={
                index === 0
                  ? 'start'
                  : index === rows.length - 1
                    ? 'end'
                    : 'middle'
              }
              fontSize={12}
              fill="var(--muted-foreground)"
            >
              {dates.format(Number(row.id) * 1000)}
            </text>
          ) : null,
        )}
      </svg>
      <p id={help} className="sr-only">
        {t(timeDetail ? 'schedulingChartKeys' : 'usageChartKeys')}
      </p>
      <div
        role="status"
        aria-live="polite"
        className="flex min-h-10 flex-wrap items-center gap-x-3 gap-y-1 text-sm"
      >
        {selected && active !== null ? (
          <>
            <span className="text-muted-foreground">
              {fullDates.format(Number(selected.id) * 1000)} · {valueLabel}
            </span>
            <strong className="font-medium tabular-nums">
              {amounts[active] === null
                ? (missingLabel ?? t('usageTokenNotReported'))
                : numbers.format(amounts[active]!)}
            </strong>
            {partialValues?.[active] && amounts[active] !== null && (
              <span className="text-xs text-muted-foreground">
                {t('usageReportedOnly')}
              </span>
            )}
          </>
        ) : (
          <span className="text-muted-foreground">
            {t(timeDetail ? 'schedulingChartHint' : 'usageChartHint')}
          </span>
        )}
      </div>
      {amounts.some((amount) => amount === null) && (
        <p className="text-xs text-muted-foreground">
          {t(timeDetail ? 'schedulingChartGaps' : 'usageChartGaps')}
        </p>
      )}
    </div>
  )
}
