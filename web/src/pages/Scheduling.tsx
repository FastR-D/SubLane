import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearch } from '@tanstack/react-router'
import { ChevronDown, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { authOptions } from '@/lib/auth'
import { accountOptions } from '@/lib/accounts'
import { channelOptions } from '@/lib/channels'
import {
  groupOptions,
  schedulingOptions,
  type SchedulingPolicy,
} from '@/lib/groups'
import { ApiError } from '@/lib/request'
import { useTimeZone } from '@/lib/timezone'
import {
  schedulingHistoryOptions,
  schedulingPolicyOptions,
  schedulingRevisionOptions,
  saveSchedulingPolicy,
  previewSchedulingPolicy,
  schedulingTimeline,
  schedulingUsageTimeline,
  schedulingPolicySchema,
  type SchedulingHistory,
} from '@/lib/scheduling'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
} from '@/components/ui/DropdownMenu'
import { TableViewport } from '@/components/TableViewport'
import { Pagination } from '@/components/Pagination'
import { TimeSeriesChart } from '@/components/TimeSeriesChart'

function Choice({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (value: string) => void
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" aria-label={label}>
          {options.find((v) => v.value === value)?.label ?? label}
          <ChevronDown aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-72 overflow-y-auto">
        <DropdownMenuRadioGroup value={value} onValueChange={onChange}>
          {options.map((v) => (
            <DropdownMenuRadioItem key={v.value} value={v.value}>
              {v.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

const stateKeys = {
  eligible: 'schedulingEligible',
  policy: 'schedulingPolicyExcluded',
  unavailable: 'availabilityUnavailable',
  busy: 'schedulingBusy',
  cooling: 'accountCooling',
  quota: 'accountQuotaExhausted',
  catalog_unknown: 'availabilityUnknown',
  model: 'schedulingModelExcluded',
  lower_tier: 'schedulingLowerTier',
} as const

export function Scheduling() {
  const { data } = useQuery(authOptions())
  return data?.user?.role === 'admin' ? (
    <SchedulingView key={data.user.id} />
  ) : null
}
function SchedulingView() {
  const { t, i18n } = useTranslation()
  const timeZone = useTimeZone()
  const search = useSearch({ from: '/groups/scheduling' })
  const groups = useQuery(groupOptions)
  const accounts = useQuery(accountOptions)
  const channels = useQuery(channelOptions)
  const [selectedGroup, setGroup] = useState(search.group ?? 0)
  const group = selectedGroup || groups.data?.groups[0]?.id || 0
  const [model, setModel] = useState(search.model ?? '')
  const [draftModel, setDraftModel] = useState(model)
  const [view, setView] = useState('current')
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const preview = useQuery({
    ...schedulingOptions(group, model),
    enabled: group > 0,
  })
  const policy = useQuery(schedulingPolicyOptions)
  const resources = [
    ...(accounts.data?.accounts ?? []),
    ...(channels.data?.channels ?? []),
  ]
  const name = (id: string) => resources.find((v) => v.id === id)?.name ?? id
  const number = (v: number) =>
    new Intl.NumberFormat(i18n.resolvedLanguage ?? 'en', {
      maximumFractionDigits: 3,
    }).format(v)
  const date = (stamp: number) =>
    new Intl.DateTimeFormat(i18n.resolvedLanguage ?? 'en', {
      dateStyle: 'medium',
      timeStyle: 'short',
      timeZone,
    }).format(stamp * 1000)
  const states =
    preview.data?.resources ??
    preview.data?.weights.map((v) => ({
      account_id: v.account_id,
      state: 'eligible' as const,
      in_flight: v.in_flight,
      max_concurrency: v.max_concurrency,
      tier: 0,
    })) ??
    []
  const currentPage = Math.min(
    page,
    Math.max(1, Math.ceil(states.length / size)),
  )
  const apply = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setModel(draftModel.trim())
    setPage(1)
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="page-title">{t('schedulingDashboard')}</h1>
          <p className="page-description">{t('schedulingDescription')}</p>
        </div>
        <Button
          variant="outline"
          disabled={preview.isFetching}
          onClick={() =>
            Promise.all([groups.refetch(), preview.refetch(), policy.refetch()])
          }
        >
          <RefreshCw aria-hidden="true" />
          {t('refresh')}
        </Button>
      </div>
      <div className="flex flex-wrap gap-3">
        <Choice
          label={t('accountGroups')}
          value={String(group)}
          options={(groups.data?.groups ?? []).map((v) => ({
            value: String(v.id),
            label: v.name,
          }))}
          onChange={(v) => {
            setGroup(Number(v))
            setPage(1)
          }}
        />
        <Choice
          label={t('schedulingDashboard')}
          value={view}
          options={[
            { value: 'current', label: t('schedulingCurrent') },
            { value: 'history', label: t('schedulingHistory') },
            { value: 'parameters', label: t('schedulingParameters') },
          ]}
          onChange={setView}
        />
      </div>
      {(groups.isError || accounts.isError || channels.isError) && (
        <p role="alert" className="text-sm text-error">
          {t('groupSchedulingFailed')}
        </p>
      )}
      {groups.isPending ? (
        <p role="status">{t('loadingGroups')}</p>
      ) : !group ? (
        <p>{t('groupsEmpty')}</p>
      ) : view === 'current' ? (
        <>
          <form onSubmit={apply} className="flex flex-wrap items-end gap-3">
            <label className="min-w-0 flex-1 space-y-2 text-sm">
              {t('groupSchedulingModel')}
              <Input
                value={draftModel}
                maxLength={160}
                onChange={(e) => setDraftModel(e.target.value)}
              />
            </label>
            <Button type="submit" variant="outline">
              {t('refresh')}
            </Button>
          </form>
          <p className="text-sm text-muted-foreground">
            {t('groupSchedulingDescription')}
          </p>
          {preview.isFetching ? (
            <p role="status">{t('loadingGroups')}</p>
          ) : preview.isError ? (
            <p role="alert" className="text-error">
              {t('groupSchedulingFailed')}
            </p>
          ) : (
            preview.data && (
              <>
                <p className="text-sm text-muted-foreground">
                  {t(
                    preview.data.dynamic
                      ? 'groupSchedulingDynamic'
                      : 'groupSchedulingStatic',
                  )}{' '}
                  {preview.data.policy && (
                    <>
                      {t('schedulingResetPreference')}:{' '}
                      {t(
                        preview.data.dynamic && preview.data.policy.enabled
                          ? 'active'
                          : 'disabled',
                      )}{' '}
                      ·{' '}
                    </>
                  )}
                  {preview.data.as_of !== undefined &&
                    t('availabilityObserved', {
                      date: date(preview.data.as_of),
                    })}
                </p>
                {states.length === 0 ? (
                  <p>{t('groupSchedulingEmpty')}</p>
                ) : (
                  <>
                    <TableViewport className="max-h-[36rem]">
                      <table className="w-full text-left text-sm">
                        <thead>
                          <tr>
                            {(
                              [
                                'groupSchedulingResource',
                                'contentRuleStatus',
                                'schedulingPressure',
                                'schedulingWeight',
                                'schedulingTarget',
                              ] as const
                            ).map((key) => (
                              <th key={key} className="px-4 py-3 font-medium">
                                {t(key)}
                              </th>
                            ))}
                          </tr>
                        </thead>
                        <tbody>
                          {states
                            .slice((currentPage - 1) * size, currentPage * size)
                            .map((v) => {
                              const w = preview.data.weights.find(
                                (w) => w.account_id === v.account_id,
                              )
                              return (
                                <tr
                                  key={v.account_id}
                                  className="border-t border-border align-top"
                                >
                                  <td className="min-w-44 px-4 py-3 font-medium">
                                    {name(v.account_id)}
                                  </td>
                                  <td className="px-4 py-3">
                                    {t(stateKeys[v.state])}
                                  </td>
                                  <td className="whitespace-nowrap px-4 py-3 tabular-nums">
                                    {number(v.in_flight)} /{' '}
                                    {number(v.max_concurrency)}
                                  </td>
                                  <td className="min-w-72 px-4 py-3">
                                    <span className="font-medium tabular-nums">
                                      {w ? number(w.effective_weight) : '—'}
                                    </span>
                                    {w && (
                                      <details className="mt-2 text-xs text-muted-foreground">
                                        <summary className="cursor-pointer">
                                          {t('schedulingFactors')}
                                        </summary>
                                        <p className="mt-2 leading-6 tabular-nums">
                                          {number(w.quota_factor)} ×{' '}
                                          {number(w.reset?.factor ?? 1)} ×{' '}
                                          {number(w.load_factor)} ×{' '}
                                          {number(1 + w.preference_boost / 100)}
                                        </p>
                                        <p>{t('schedulingEquation')}</p>
                                        {w.quota_state !== 'available' && (
                                          <p>
                                            {t(
                                              w.quota_state === 'unsupported'
                                                ? 'groupSchedulingUnsupported'
                                                : 'groupSchedulingFallback',
                                            )}
                                          </p>
                                        )}

                                        <p>
                                          {w.reset?.reset_at
                                            ? t('allocationResetAt', {
                                                date: date(w.reset.reset_at),
                                              })
                                            : t('schedulingTimingUnknown')}
                                        </p>
                                        {w.quota_updated_at && (
                                          <p>
                                            {t('availabilityObserved', {
                                              date: date(w.quota_updated_at),
                                            })}
                                          </p>
                                        )}
                                        {w.reset?.remaining_percent != null && (
                                          <p>
                                            {t('allocationRemaining')}:{' '}
                                            {number(w.reset.remaining_percent)}%
                                          </p>
                                        )}
                                      </details>
                                    )}
                                  </td>
                                  <td className="min-w-32 px-4 py-3 tabular-nums">
                                    {w
                                      ? `${number(w.target_share * 100)}%`
                                      : '—'}
                                    {w && (
                                      <div
                                        aria-hidden="true"
                                        className="mt-2 h-2 overflow-hidden rounded-full bg-muted"
                                      >
                                        <div
                                          className="h-full rounded-full bg-foreground"
                                          style={{
                                            width: `${w.target_share * 100}%`,
                                          }}
                                        />
                                      </div>
                                    )}
                                  </td>
                                </tr>
                              )
                            })}
                        </tbody>
                      </table>
                    </TableViewport>
                    <Pagination
                      page={currentPage}
                      pageSize={size}
                      total={states.length}
                      onPageChange={setPage}
                      onPageSizeChange={(v) => {
                        setSize(v)
                        setPage(1)
                      }}
                    />
                  </>
                )}
              </>
            )
          )}
        </>
      ) : view === 'history' ? (
        <SchedulingHistoryView
          key={group}
          group={group}
          resources={resources.filter((r) =>
            states.some((v) => v.account_id === r.id),
          )}
        />
      ) : policy.isPending ? (
        <p role="status">{t('loadingGroups')}</p>
      ) : policy.isError ? (
        <p role="alert" className="text-error">
          {t('groupSchedulingFailed')}
        </p>
      ) : (
        policy.data && (
          <SchedulingParameters
            policy={policy.data.policy}
            dynamic={policy.data.dynamic}
            group={group}
            model={model}
            name={name}
          />
        )
      )}
    </div>
  )
}

function SchedulingParameters({
  policy,
  dynamic,
  group,
  model,
  name,
}: {
  policy: SchedulingPolicy
  dynamic: boolean
  group: number
  model: string
  name: (id: string) => string
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [draft, setDraft] = useState(policy)
  const revisions = useQuery(schedulingRevisionOptions)
  const save = useMutation({
    mutationFn: saveSchedulingPolicy,
    onSuccess: async (value) => {
      setDraft(value)
      await Promise.all([
        client.invalidateQueries({ queryKey: ['scheduling-policy'] }),
        client.invalidateQueries({ queryKey: ['group-scheduling'] }),
        client.invalidateQueries({ queryKey: ['scheduling-revisions'] }),
      ])
    },
  })
  const preview = useMutation({
    mutationFn: () => previewSchedulingPolicy(group, model, draft),
  })
  const review = useMutation({
    mutationFn: () =>
      client.fetchQuery({ ...schedulingPolicyOptions, staleTime: 0 }),
    onSuccess: (value) => {
      setDraft(value.policy)
      save.reset()
      preview.reset()
    },
  })
  const updateDraft = (value: SchedulingPolicy) => {
    setDraft(value)
    save.reset()
    preview.reset()
  }
  const valid = schedulingPolicySchema.safeParse(draft).success
  const fields = [
    ['max_multiplier', 'schedulingMaxMultiplier', 1, 2, 0.05],
    ['horizon_fraction', 'schedulingHorizonFraction', 0.01, 1, 0.01],
    ['max_horizon_seconds', 'schedulingMaxHorizon', 60, 604800, 60],
    ['curve_power', 'schedulingCurvePower', 1, 4, 0.1],
  ] as const
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    save.mutate(draft)
  }
  return (
    <section className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">{t('schedulingParameters')}</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          {t('schedulingDraftHint')}
        </p>
        <p className="mt-2 text-sm">
          {t('schedulingRevision', { revision: policy.revision })} ·{' '}
          {t(dynamic ? 'groupSchedulingDynamic' : 'groupSchedulingStatic')}
        </p>
      </div>
      <form onSubmit={submit} className="max-w-3xl space-y-5">
        <label className="flex items-center gap-3 text-sm">
          <input
            type="checkbox"
            className="size-4 accent-primary"
            checked={draft.enabled}
            onChange={(e) =>
              updateDraft({ ...draft, enabled: e.target.checked })
            }
          />
          {t('schedulingResetPreference')}
        </label>
        <div className="grid gap-4 sm:grid-cols-2">
          {fields.map(([key, label, min, max, step]) => (
            <label key={key} className="space-y-2 text-sm">
              {t(label)}
              <Input
                type="number"
                min={min}
                max={max}
                step={step}
                required
                value={Number.isFinite(draft[key]) ? draft[key] : ''}
                onChange={(e) =>
                  updateDraft({ ...draft, [key]: e.target.valueAsNumber })
                }
              />
            </label>
          ))}
        </div>
        <p className="text-sm text-muted-foreground">
          {t('schedulingEquation')}
        </p>
        <TableViewport>
          <table className="w-full text-left text-sm tabular-nums">
            <caption className="pb-2 text-left text-muted-foreground">
              {t('schedulingCurveHint')}
            </caption>
            <thead>
              <tr>
                <th className="px-3 py-2">{t('schedulingTimeLeft')}</th>
                <th className="px-3 py-2">{t('schedulingMultiplier')}</th>
              </tr>
            </thead>
            <tbody>
              {[1, 0.75, 0.5, 0.25, 0.01].map((v) => (
                <tr key={v} className="border-t border-border">
                  <td className="px-3 py-2">{v * 100}%</td>
                  <td className="px-3 py-2">
                    {valid
                      ? (
                          1 +
                          (draft.enabled ? draft.max_multiplier - 1 : 0) *
                            Math.pow(1 - v, draft.curve_power)
                        ).toFixed(3)
                      : '—'}
                    ×
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableViewport>
        <div className="flex flex-wrap gap-3">
          <Button
            type="submit"
            disabled={save.isPending || !valid || review.isPending}
          >
            {t('schedulingSave')}
          </Button>
          <Button
            variant="outline"
            type="button"
            disabled={preview.isPending || !valid || save.isPending}
            onClick={() => preview.mutate()}
          >
            {t('schedulingPreview')}
          </Button>
          <Button
            variant="outline"
            type="button"
            disabled={review.isPending}
            onClick={() => review.mutate()}
          >
            {t('allocationReviewLatest')}
          </Button>
        </div>
        {review.isError && (
          <p role="alert" className="text-sm text-error">
            {t('groupSchedulingFailed')}
          </p>
        )}
        {save.isError && (
          <p role="alert" className="text-sm text-error">
            {t(
              save.error instanceof ApiError &&
                save.error.code === 'scheduling_policy_conflict'
                ? 'schedulingConflict'
                : 'groupSchedulingFailed',
            )}
          </p>
        )}
        {save.isSuccess && (
          <p role="status" className="text-sm text-success">
            {t('schedulingSaved')}
          </p>
        )}
      </form>
      {preview.isError ? (
        <p role="alert" className="text-sm text-error">
          {t('groupSchedulingFailed')}
        </p>
      ) : (
        preview.isSuccess && (
          <div className="space-y-3">
            <h3 className="font-medium">{t('schedulingPreview')}</h3>
            {preview.data.weights.map((w) => (
              <p key={w.account_id} className="text-sm tabular-nums">
                {name(w.account_id)}:{' '}
                {preview.data.baseline
                  ?.find((b) => b.account_id === w.account_id)
                  ?.effective_weight.toFixed(3)}{' '}
                →{' '}
                {t('groupSchedulingWeight', {
                  weight: w.effective_weight.toFixed(3),
                  boost: w.preference_boost,
                })}{' '}
                ·{' '}
                {preview.data.baseline?.find(
                  (b) => b.account_id === w.account_id,
                ) &&
                  `${((preview.data.baseline.find((b) => b.account_id === w.account_id)?.target_share ?? 0) * 100).toFixed(1)}% → `}
                {t('groupSchedulingShare', {
                  share: (w.target_share * 100).toFixed(1),
                })}
              </p>
            ))}
            {!preview.data.weights.length && <p>{t('groupSchedulingEmpty')}</p>}
          </div>
        )
      )}
      <div className="space-y-3 border-t border-border pt-6">
        <h3 className="font-medium">{t('schedulingPolicyHistory')}</h3>
        {revisions.isPending ? (
          <p role="status">{t('loadingGroups')}</p>
        ) : revisions.isError ? (
          <p role="alert" className="text-error">
            {t('groupSchedulingFailed')}
          </p>
        ) : revisions.data?.revisions.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {t('schedulingNoHistory')}
          </p>
        ) : (
          revisions.data?.revisions.map((v) => (
            <div
              key={v.revision}
              className="flex flex-wrap items-center justify-between gap-3 border-b border-border py-3 text-sm"
            >
              <span>
                {t('schedulingRevision', { revision: v.revision })} ·{' '}
                {v.max_multiplier}× · {v.horizon_fraction * 100}%
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  updateDraft({ ...v, revision: draft.revision })
                  save.reset()
                  preview.reset()
                }}
              >
                {t('schedulingRestore')}
              </Button>
            </div>
          ))
        )}
      </div>
    </section>
  )
}

function SchedulingHistoryView({
  group,
  resources,
}: {
  group: number
  resources: { id: string; name: string }[]
}) {
  const { t, i18n } = useTranslation()
  const timeZone = useTimeZone()
  const [hours, setHours] = useState(24)
  const [account, setAccount] = useState('')
  const [metric, setMetric] = useState('pressure')
  const [window, setWindow] = useState('')
  const [scope, setScope] = useState('')
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(10)
  const query = useQuery(schedulingHistoryOptions(group, hours, account))
  const number = (v: number) =>
    new Intl.NumberFormat(i18n.resolvedLanguage ?? 'en', {
      maximumFractionDigits: 2,
    }).format(v)
  const date = (v: number) =>
    new Intl.DateTimeFormat(i18n.resolvedLanguage ?? 'en', {
      dateStyle: 'medium',
      timeStyle: 'short',
      timeZone,
    }).format(v * 1000)
  const windows = [
    ...new Set(
      query.data?.samples
        .filter((v) => v.kind === 'quota')
        .map((v) => v.scope) ?? [],
    ),
  ]
  const selectedWindow = windows.includes(window) ? window : (windows[0] ?? '')
  const scopes = [
    ...new Set(
      (query.data?.samples ?? [])
        .filter((v) => v.kind === 'placement' && v.account_id === account)
        .map((v) => v.scope),
    ),
  ]
  const selectedScope = scopes.includes(scope) ? scope : (scopes[0] ?? '')
  const samples = (query.data?.samples ?? []).filter(
    (v) =>
      v.account_id === account &&
      (metric !== 'quota' || v.scope === selectedWindow) &&
      (metric !== 'placement' || v.scope === selectedScope),
  )
  const diagnostic = ['pressure', 'quota', 'placement'].includes(metric)
  const timeline = diagnostic
    ? schedulingTimeline(samples, metric as 'pressure' | 'quota' | 'placement')
    : schedulingUsageTimeline(
        (query.data?.usage ?? []).filter((v) => v.account_id === account),
        metric,
      )
  const totals = aggregateHistory(query.data)
  const currentPage = Math.min(
    page,
    Math.max(1, Math.ceil(totals.length / size)),
  )
  const labels = {
    pressure: 'schedulingPressure',
    quota: 'schedulingRemainingPercent',
    placement: 'schedulingWeight',
    requests: 'usageRequests',
    input: 'usageInputTokens',
    output: 'usageOutputTokens',
    duration: 'schedulingDuration',
    wait: 'schedulingWait',
    first_output: 'schedulingFirstOutput',
    errors: 'schedulingErrors',
    limited: 'schedulingLimited',
  } as const
  const label = t(labels[metric as keyof typeof labels])
  return (
    <section className="space-y-5">
      <div className="flex flex-wrap gap-3">
        <Choice
          label={t('usagePeriod')}
          value={String(hours)}
          options={[1, 24, 168, 720, 2160].map((v) => ({
            value: String(v),
            label: t('allocationDurationHours', { value: v }),
          }))}
          onChange={(v) => {
            setHours(Number(v))
            setPage(1)
          }}
        />
        <Choice
          label={t('groupSchedulingResource')}
          value={account}
          options={[
            { value: '', label: t('schedulingChooseResource') },
            ...resources.map((v) => ({ value: v.id, label: v.name })),
          ]}
          onChange={(v) => {
            setAccount(v)
            setPage(1)
          }}
        />
        <Choice
          label={t('usageMetric')}
          value={metric}
          options={[
            { value: 'pressure', label: t('schedulingPressure') },
            { value: 'quota', label: t('schedulingRemainingPercent') },
            { value: 'placement', label: t('schedulingWeight') },
            ...(
              [
                'requests',
                'input',
                'output',
                'duration',
                'wait',
                'first_output',
                'errors',
                'limited',
              ] as const
            ).map((v) => ({ value: v, label: t(labels[v]) })),
          ]}
          onChange={setMetric}
        />
        <Button
          variant="outline"
          disabled={query.isFetching}
          onClick={() => query.refetch()}
        >
          {t('refresh')}
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        {t('schedulingHistoryHint')}
      </p>
      {metric === 'quota' && windows.length > 0 && (
        <Choice
          label={t('schedulingWindow')}
          value={selectedWindow}
          options={windows.map((v) => ({ value: v, label: v }))}
          onChange={setWindow}
        />
      )}
      {metric === 'placement' && scopes.length > 0 && (
        <Choice
          label={t('schedulingScope')}
          value={selectedScope}
          options={scopes.map((v) => ({ value: v, label: v }))}
          onChange={setScope}
        />
      )}
      {query.isPending ? (
        <p role="status">{t('loadingGroups')}</p>
      ) : query.isError ? (
        <p role="alert" className="text-error">
          {t('groupSchedulingFailed')}
        </p>
      ) : (
        query.data && (
          <>
            <p className="text-xs text-muted-foreground">
              {query.data.collected_since
                ? t('schedulingCoverage', {
                    date: date(query.data.collected_since),
                    flush: date(query.data.last_flush),
                  })
                : t('schedulingNoHistory')}
            </p>
            {(query.data.truncated || query.data.dropped > 0) && (
              <p role="status" className="text-sm text-warning">
                {t('schedulingPartial', { count: query.data.dropped })}
              </p>
            )}
            {account && timeline.length ? (
              <TimeSeriesChart
                rows={timeline}
                amounts={timeline.map((v) => v.value)}
                partialValues={timeline.map(
                  (v) => 'partial' in v && v.partial === true,
                )}
                valueLabel={label}
                label={label}
                timeZone={timeZone}
                timeDetail
                missingLabel={t('availabilityUnknown')}
              />
            ) : (
              <p className="py-8 text-sm text-muted-foreground">
                {t(
                  account ? 'schedulingNoHistory' : 'schedulingChooseResource',
                )}
              </p>
            )}
            <p className="text-xs text-muted-foreground">
              {t('schedulingExpectedHint')}
            </p>
            {totals.length > 0 && (
              <>
                <TableViewport className="max-h-96">
                  <table className="w-full text-left text-sm">
                    <thead>
                      <tr>
                        {[
                          'groupSchedulingResource',
                          'usageRequests',
                          'schedulingExpected',
                          'schedulingActual',
                          'schedulingLimited',
                          'schedulingWait',
                        ].map((k) => (
                          <th key={k} className="whitespace-nowrap px-4 py-3">
                            {t(k as 'usageRequests')}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {totals
                        .slice((currentPage - 1) * size, currentPage * size)
                        .map((v) => (
                          <tr key={v.id} className="border-t border-border">
                            <td className="px-4 py-3">
                              {resources.find((r) => r.id === v.id)?.name ??
                                v.id}
                            </td>
                            <td className="px-4 py-3 tabular-nums">
                              {number(v.requests)}
                            </td>
                            <td className="px-4 py-3 tabular-nums">
                              {query.data.truncated || query.data.dropped
                                ? '—'
                                : number(v.expected)}
                            </td>
                            <td className="px-4 py-3 tabular-nums">
                              {number(v.selected)}
                            </td>
                            <td className="px-4 py-3 tabular-nums">
                              {number(v.limited)}
                            </td>
                            <td className="px-4 py-3 tabular-nums">
                              {v.requests ? number(v.wait / v.requests) : '—'}
                            </td>
                          </tr>
                        ))}
                    </tbody>
                  </table>
                </TableViewport>
                <Pagination
                  page={currentPage}
                  pageSize={size}
                  total={totals.length}
                  onPageChange={setPage}
                  onPageSizeChange={(v) => {
                    setSize(v)
                    setPage(1)
                  }}
                />
              </>
            )}
            <div className="border-t border-border pt-5">
              <h3 className="font-medium">{t('schedulingEvaluation')}</h3>
              <p className="mt-2 text-sm text-muted-foreground">
                {t('schedulingInsufficient')}
              </p>
              <p className="mt-2 text-sm">
                {t('schedulingCycleCoverage', {
                  complete: query.data.evaluation.complete_cycles,
                  observed: query.data.evaluation.observed_cycles,
                  cohorts: query.data.evaluation.cohorts,
                })}
              </p>
              {query.data.evaluation.recommended_revision !== null && (
                <p className="mt-2 text-sm">
                  {t('schedulingRecommendation', {
                    revision: query.data.evaluation.recommended_revision,
                  })}
                </p>
              )}
              {query.data.evaluation.reports.length > 0 && (
                <TableViewport>
                  <table className="mt-3 w-full text-left text-sm">
                    <thead>
                      <tr>
                        {[
                          'schedulingParameters',
                          'schedulingCycles',
                          'schedulingEndBalance',
                        ].map((k) => (
                          <th key={k} className="px-4 py-3">
                            {t(k as 'schedulingParameters')}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {query.data.evaluation.reports.map((v) => (
                        <tr key={v.revision} className="border-t border-border">
                          <td className="px-4 py-3">
                            {t('schedulingRevision', { revision: v.revision })}
                            {!v.comparable && (
                              <span className="ml-2 text-muted-foreground">
                                {t('schedulingLimitedEvidence')}
                              </span>
                            )}
                          </td>
                          <td className="px-4 py-3 tabular-nums">
                            {number(v.cycles)}
                          </td>
                          <td className="px-4 py-3 tabular-nums">
                            {number(v.mean_remaining)}%
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </TableViewport>
              )}
              {query.data.cycles.length > 0 && (
                <details className="mt-4 text-sm">
                  <summary className="cursor-pointer">
                    {t('schedulingResetEvidence')}
                  </summary>
                  <TableViewport className="mt-3 max-h-96">
                    <table className="w-full text-left">
                      <thead>
                        <tr>
                          {[
                            'groupSchedulingResource',
                            'schedulingWindow',
                            'allocationResetTime',
                            'schedulingEndBalance',
                            'contentRuleStatus',
                          ].map((k) => (
                            <th key={k} className="px-4 py-3">
                              {t(k as 'schedulingWindow')}
                            </th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {query.data.cycles.map((v) => (
                          <tr
                            key={`${v.account_id}:${v.scope}:${v.lifecycle}:${v.reset_at}`}
                            className="border-t border-border"
                          >
                            <td className="px-4 py-3">
                              {resources.find((r) => r.id === v.account_id)
                                ?.name ?? v.account_id}
                            </td>
                            <td className="px-4 py-3">{v.scope}</td>
                            <td className="px-4 py-3">{date(v.reset_at)}</td>
                            <td className="px-4 py-3 tabular-nums">
                              {number(v.remaining)}%
                              <span className="block text-xs text-muted-foreground">
                                {t('schedulingObservationAge', {
                                  seconds: v.observation_age,
                                })}
                              </span>
                            </td>
                            <td className="px-4 py-3">
                              {t(
                                v.complete
                                  ? 'schedulingComparableCycle'
                                  : 'schedulingLimitedEvidence',
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </TableViewport>
                </details>
              )}
            </div>
          </>
        )
      )}
    </section>
  )
}
function aggregateHistory(history?: SchedulingHistory) {
  const totals = new Map<
    string,
    {
      id: string
      requests: number
      expected: number
      selected: number
      limited: number
      wait: number
    }
  >()
  const get = (id: string) => {
    if (!totals.has(id))
      totals.set(id, {
        id,
        requests: 0,
        expected: 0,
        selected: 0,
        limited: 0,
        wait: 0,
      })
    return totals.get(id)!
  }
  for (const v of history?.usage ?? []) {
    const r = get(v.account_id)
    r.requests += v.requests
    r.limited += v.limited
    r.wait += v.wait_ms
  }
  for (const v of history?.samples ?? []) {
    if (v.kind !== 'placement') continue
    const r = get(v.account_id)
    r.expected += v.expected
    r.selected += v.selected
  }
  return [...totals.values()]
}
