import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Info, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  refreshUsage,
  usageOptions,
  usagePace,
  usageReasonKey,
  type UsagePace,
  type UsageResource,
  type UsageWindow,
} from '@/lib/usage'
import { cn } from '@/lib/cn'
import { Button } from './ui/Button'
import { useTimeZone } from '@/lib/timezone'
import { ApiError } from '@/lib/request'
import type { Provider } from '@/lib/accounts'

export function AccountUsage({
  id,
  name,
  provider,
  resource = 'accounts',
}: {
  id: string
  name: string
  provider: Provider
  resource?: UsageResource
}) {
  const { t, i18n } = useTranslation()
  const timeZone = useTimeZone()
  const client = useQueryClient()
  const query = useQuery(usageOptions(id, resource))
  const refresh = useMutation({
    mutationFn: () => refreshUsage(id, resource),
    onSuccess: (data) =>
      client.setQueryData(usageOptions(id, resource).queryKey, data),
  })
  const [now, setNow] = useState(() => Date.now())
  const [expanded, setExpanded] = useState(false)
  const limits = query.data?.limits ?? []
  const modelQuotas = provider === 'antigravity'
  // Surface constrained models before folding the rest; ordering never combines distinct quotas.
  const ordered = modelQuotas
    ? [...limits].sort(
        (a, b) =>
          (b.windows[0]?.used_percent ?? -1) -
          (a.windows[0]?.used_percent ?? -1),
      )
    : limits
  const visible = modelQuotas && !expanded ? ordered.slice(0, 3) : ordered
  const cooldown = Math.max(
    0,
    (query.data?.retry_after_seconds ?? 0) -
      Math.floor(Math.max(0, now - query.dataUpdatedAt) / 1000),
  )
  const coolingDown = cooldown > 0
  useEffect(() => {
    const timer = window.setInterval(
      () => setNow(Date.now()),
      coolingDown ? 1000 : 30_000,
    )
    return () => window.clearInterval(timer)
  }, [coolingDown])
  const locale = i18n.resolvedLanguage ?? 'en'
  // Use the response clock, not the observation time: persisted snapshots may be hours old after a restart.
  const snapshotNow = query.data
    ? query.data.server_time * 1000 + Math.max(0, now - query.dataUpdatedAt)
    : now
  const refreshing =
    refresh.isPending ||
    (!query.isError && !refresh.isError && query.data?.refreshing === true)
  const failed =
    query.isError || refresh.isError || query.data?.refresh_failed === true
  const refreshError =
    refresh.error instanceof ApiError
      ? refresh.error
      : query.error instanceof ApiError
        ? query.error
        : query.data?.refresh_error
  const reason = t(usageReasonKey(refreshError))
  const stale =
    query.data &&
    (query.data.stale || snapshotNow >= query.data.expires_at * 1000)
  return (
    <section
      aria-label={t('accountUsageLabel', { name })}
      className="min-w-0 space-y-3"
    >
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <h3 className="text-xs font-medium text-muted-foreground">
          {t('accountUsage')}
        </h3>
        {query.data && provider === 'codex' && (
          <p className="text-xs tabular-nums text-muted-foreground">
            {t('usageResetCredits', {
              count:
                query.data.reset_credits === null
                  ? t('usageResetCreditsUnknown')
                  : new Intl.NumberFormat(locale).format(
                      query.data.reset_credits,
                    ),
            })}
          </p>
        )}
      </div>
      {query.isPending && (
        <p role="status" className="text-xs text-muted-foreground">
          {t('usageLoading')}
        </p>
      )}
      {failed ? (
        <p role="status" className="text-xs text-warning">
          {t(query.data ? 'usageStale' : 'usageLoadFailed', { reason })}
        </p>
      ) : stale ? (
        <p role="status" className="text-xs text-muted-foreground">
          {t(refreshing ? 'usageRefreshingCached' : 'usageExpired')}
        </p>
      ) : null}
      {query.data && (
        <div
          id={`usage-limits-${id}`}
          className={modelQuotas ? 'space-y-3' : 'space-y-4'}
        >
          {query.data.limits.length === 0 && (
            <p className="text-xs text-muted-foreground">
              {t('usageNotReported')}
            </p>
          )}
          {visible.map((limit, index) => (
            <div key={index} className="space-y-3">
              {limit.name && !modelQuotas && (
                <p className="break-words text-xs font-medium">{limit.name}</p>
              )}
              {(limit.allowed === false || limit.limit_reached === true) && (
                <p className="text-xs text-warning">{t('usageLimited')}</p>
              )}
              {limit.windows.length === 0 && (
                <p className="text-xs text-muted-foreground">
                  {t('usageNotReported')}
                </p>
              )}
              <div className="space-y-3">
                {limit.windows.map((window) => (
                  <QuotaWindow
                    key={window.kind}
                    window={window}
                    now={snapshotNow}
                    name={modelQuotas ? limit.name : undefined}
                    pace={
                      query.data && !failed
                        ? usagePace(window, query.data, snapshotNow / 1000)
                        : null
                    }
                  />
                ))}
              </div>
            </div>
          ))}
          {modelQuotas && limits.length > 3 && (
            <Button
              variant="ghost"
              size="sm"
              className="h-auto px-0 py-1 text-xs text-muted-foreground [@media(pointer:coarse)]:min-h-11"
              aria-expanded={expanded}
              aria-controls={`usage-limits-${id}`}
              onClick={() => setExpanded((value) => !value)}
            >
              {t(expanded ? 'usageModelsCollapse' : 'usageModelsExpand', {
                count: limits.length,
              })}
            </Button>
          )}
        </div>
      )}
      <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
        {query.data ? (
          <time
            dateTime={new Date(query.data.updated_at * 1000).toISOString()}
            className="min-w-0 leading-5"
          >
            {t('usageUpdated', {
              time: new Intl.DateTimeFormat(locale, {
                timeZone,
                dateStyle: 'short',
                timeStyle: 'short',
              }).format(query.data.updated_at * 1000),
            })}
          </time>
        ) : (
          <span className="sr-only">{t('accountUsage')}</span>
        )}
        <div className="flex shrink-0 items-center gap-1">
          {coolingDown && !refreshing && (
            <span className="tabular-nums">
              {t('usageRetryIn', { seconds: cooldown })}
            </span>
          )}
          <Button
            variant="ghost"
            size="icon"
            className="size-7 text-muted-foreground [@media(pointer:coarse)]:size-11"
            disabled={query.isFetching || refreshing || coolingDown}
            title={t('refreshUsage')}
            aria-label={t('refreshAccountUsage', { name })}
            onClick={() => refresh.mutate()}
          >
            <RefreshCw
              aria-hidden="true"
              className={cn(
                'size-3.5',
                (query.isFetching || refreshing) && 'motion-safe:animate-spin',
              )}
            />
          </Button>
        </div>
      </div>
    </section>
  )
}

function QuotaWindow({
  window,
  now,
  name,
  pace,
}: {
  window: UsageWindow
  now: number
  name?: string
  pace: UsagePace | null
}) {
  const { t, i18n } = useTranslation()
  const timeZone = useTimeZone()
  const locale = i18n.resolvedLanguage ?? 'en'
  const seconds = window.window_seconds
  const label =
    name ??
    (seconds === null
      ? t(
          window.kind === 'model'
            ? 'usageModel'
            : window.kind === 'primary'
              ? 'usagePrimary'
              : window.kind === 'monthly'
                ? 'usageMonthly'
                : 'usageSecondary',
        )
      : seconds % 86400 === 0
        ? t('usageDays', { count: seconds / 86400 })
        : seconds % 3600 === 0
          ? t('usageHours', { count: seconds / 3600 })
          : seconds % 60 === 0
            ? t('usageMinutes', { count: seconds / 60 })
            : t('usageSeconds', { count: seconds }))
  const remaining =
    window.used_percent === null
      ? null
      : Math.max(0, Math.min(100, 100 - window.used_percent))
  const tone =
    remaining === null
      ? 'text-muted-foreground'
      : remaining < 5
        ? 'text-error'
        : remaining < 20
          ? 'text-warning'
          : 'text-success'
  const percentage =
    remaining === null
      ? t('usageUnknown')
      : t('usageRemaining', {
          percent: new Intl.NumberFormat(locale, {
            maximumFractionDigits: 2,
          }).format(remaining),
        })
  const elapsed = window.reset_at !== null && window.reset_at * 1000 <= now
  const resetSeconds =
    window.reset_at === null
      ? null
      : Math.max(0, window.reset_at - Math.floor(now / 1000))
  const minutes = Math.ceil((resetSeconds ?? 0) / 60)
  const resetText =
    resetSeconds === null
      ? t('usageResetUnknown')
      : elapsed
        ? t('usageResetPassed')
        : minutes >= 1440
          ? t('usageResetDays', {
              days: Math.floor(minutes / 1440),
              hours: Math.floor((minutes % 1440) / 60),
            })
          : minutes >= 60
            ? t('usageResetHours', {
                hours: Math.floor(minutes / 60),
                minutes: minutes % 60,
              })
            : t('usageResetMinutes', { minutes })
  return (
    <div className="space-y-1.5 text-xs">
      <div className="flex items-center justify-between gap-3">
        <span
          title={name}
          className={cn(
            'min-w-0 truncate',
            name ? 'font-medium' : 'text-muted-foreground',
          )}
        >
          {label}
        </span>
        <span className={cn('shrink-0 tabular-nums', tone)}>{percentage}</span>
      </div>
      {remaining !== null && (
        <div
          role="progressbar"
          aria-label={label}
          aria-valuenow={remaining}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuetext={percentage}
          className="h-1 overflow-hidden rounded-full bg-muted"
        >
          <div
            className={cn('h-full rounded-full bg-current', tone)}
            style={{ width: `${remaining}%` }}
          />
        </div>
      )}
      {/* A passed reset timestamp is not evidence that the provider has replenished quota. */}
      <p
        className="text-muted-foreground"
        title={
          window.reset_at === null
            ? undefined
            : new Intl.DateTimeFormat(locale, {
                timeZone,
                dateStyle: 'medium',
                timeStyle: 'short',
              }).format(window.reset_at * 1000)
        }
      >
        {resetText}
      </p>
      {pace && (
        <details className="min-w-0 text-muted-foreground">
          <summary className="flex min-w-0 cursor-pointer items-center gap-1.5 rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring [@media(pointer:coarse)]:min-h-11">
            <span className="min-w-0 break-words tabular-nums">
              {t(
                pace.direction === 'ahead'
                  ? 'usagePaceAhead'
                  : pace.direction === 'behind'
                    ? 'usagePaceBehind'
                    : 'usagePaceOnTrack',
                {
                  points: new Intl.NumberFormat(locale, {
                    maximumFractionDigits: 1,
                  }).format(pace.points),
                },
              )}
            </span>
            <Info aria-hidden="true" className="size-3.5 shrink-0" />
            <span className="sr-only">{t('usagePaceDetails')}</span>
          </summary>
          <p className="mt-1.5 leading-5">{t('usagePaceExplanation')}</p>
        </details>
      )}
    </div>
  )
}
