import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Clock3, Plus, Trash2 } from 'lucide-react'
import {
  schemesOptions,
  availableAllocationPools,
  saveScheme,
  previewInitial,
  setSchemeEnabled,
  deleteScheme,
  allocationDeleteErrorKey,
  modeLabels,
  allocationErrorKey,
  type Scheme,
  type SchemeInput,
} from '@/lib/allocations'
import { groupOptions, poolMembersOptions } from '@/lib/groups'
import { SchemeForm } from '@/components/SchemeForm'
import { AllocationPriceWarning } from '@/components/AllocationPriceWarning'
import { AllocationReport } from '@/components/AllocationReport'
import { AllocationSchedule } from '@/components/AllocationSchedule'
import { ConfirmDelete } from '@/components/ConfirmDelete'
import { Button } from '@/components/ui/Button'
import { Status } from '@/components/Status'
import { formatInstanceDate, useTimeZone } from '@/lib/timezone'

export function Allocations() {
  const { t, i18n } = useTranslation()
  const timeZone = useTimeZone()
  const query = useQuery(schemesOptions)
  const pools = useQuery(groupOptions)
  const client = useQueryClient()
  const [editing, setEditing] = useState<Scheme | null | undefined>()
  const [viewing, setViewing] = useState<number | null>(null)
  const [removing, setRemoving] = useState<Scheme | null>(null)
  const [formPoolID, setFormPoolID] = useState(0)
  const [reviewGeneration, setReviewGeneration] = useState(0)
  const [creationPreview, setCreationPreview] = useState<{
    input: SchemeInput
    at: number
  } | null>(null)
  const saved = async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ['allocations'] }),
      client.invalidateQueries({ queryKey: ['allocation'] }),
      client.invalidateQueries({ queryKey: ['own-allocations'] }),
      client.invalidateQueries({ queryKey: ['available-groups'] }),
      client.invalidateQueries({ queryKey: ['keys'] }),
    ])
    setEditing(undefined)
    setCreationPreview(null)
  }
  const mutation = useMutation({ mutationFn: saveScheme, onSuccess: saved })
  const preparing = useMutation({
    mutationFn: async (input: SchemeInput) => ({
      input,
      at: (await previewInitial(input)).proposed_effective_at,
    }),
    onSuccess: setCreationPreview,
  })
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      setSchemeEnabled(id, enabled),
    onSuccess: saved,
  })
  const freePools = availableAllocationPools(
    pools.data?.groups ?? [],
    query.data?.schemes ?? [],
  )
  const roster = useQuery({
    ...poolMembersOptions(formPoolID),
    enabled: editing !== undefined && formPoolID > 0,
  })
  const loading = query.isPending || pools.isPending
  const failed = query.isError || pools.isError
  const editingScheme = editing
    ? (query.data?.schemes.find((scheme) => scheme.id === editing.id) ??
      editing)
    : undefined
  const date = (n: number) =>
    formatInstanceDate(n * 1000, i18n.resolvedLanguage ?? 'en', timeZone, {
      dateStyle: 'short',
      timeStyle: 'medium',
    })
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="page-title">{t('allocationSchemes')}</h1>
          <p className="page-description">{t('allocationsDescription')}</p>
        </div>
        {editing === undefined && viewing === null && (
          <Button
            disabled={loading || failed || !freePools.length}
            onClick={() => {
              mutation.reset()
              setFormPoolID(freePools[0].id)
              setEditing(null)
            }}
          >
            <Plus aria-hidden="true" />
            {t('allocationCreate')}
          </Button>
        )}
      </div>
      {!loading &&
        !failed &&
        editing === undefined &&
        viewing === null &&
        !freePools.length && (
          <p className="text-sm leading-6 text-muted-foreground">
            {t('allocationPrerequisite')}{' '}
            <Link className="underline underline-offset-4" to="/groups">
              {t('accountGroups')}
            </Link>
          </p>
        )}
      {toggle.isError && (
        <p role="alert" className="text-sm text-error">
          {t(allocationErrorKey(toggle.error))}
        </p>
      )}
      {loading ? (
        <p role="status">{t('allocationLoading')}</p>
      ) : failed ? (
        <div role="alert" className="space-y-3">
          <p>{t('allocationFailed')}</p>
          <Button
            variant="outline"
            onClick={() => Promise.all([query.refetch(), pools.refetch()])}
          >
            {t('reconnect')}
          </Button>
        </div>
      ) : editing !== undefined ? (
        <section className="max-w-3xl space-y-5">
          <h2 className="font-medium">
            {t(editing ? 'allocationEdit' : 'allocationCreate')}
          </h2>
          <SchemeForm
            key={`${editing?.id ?? 'new'}:${reviewGeneration}`}
            groups={editing ? pools.data.groups : freePools}
            groupID={formPoolID}
            onGroupChange={setFormPoolID}
            members={roster.data?.members ?? []}
            scheme={editingScheme}
            onSubmit={(input) => {
              if (!editing && input.start_next) {
                preparing.mutate(input)
              } else {
                mutation.mutate({ ...input, id: editing?.id })
              }
            }}
            onCancel={() => {
              setEditing(undefined)
              setCreationPreview(null)
              preparing.reset()
            }}
            pending={
              mutation.isPending ||
              preparing.isPending ||
              creationPreview !== null ||
              roster.isPending ||
              roster.isError
            }
          />
          {preparing.isError && (
            <p role="alert" className="text-sm text-error">
              {t(allocationErrorKey(preparing.error))}
            </p>
          )}
          {creationPreview && (
            <div className="space-y-3 border-t border-border pt-4">
              <p role="status" className="text-sm">
                {t('allocationStartEffectivePreview', {
                  date: date(creationPreview.at),
                })}
              </p>
              <div className="flex flex-wrap gap-2">
                <Button
                  disabled={mutation.isPending}
                  onClick={() =>
                    mutation.mutate({
                      ...creationPreview.input,
                      expected_effective_at: creationPreview.at,
                    })
                  }
                >
                  {t('allocationConfirmStart')}
                </Button>
                <Button
                  variant="ghost"
                  disabled={mutation.isPending}
                  onClick={() => {
                    setCreationPreview(null)
                    preparing.reset()
                    mutation.reset()
                  }}
                >
                  {t('cancel')}
                </Button>
              </div>
            </div>
          )}
          {roster.isError && (
            <div
              role="alert"
              className="flex items-center gap-3 text-sm text-error"
            >
              {t('allocationRosterFailed')}
              <Button
                variant="outline"
                size="sm"
                onClick={() => roster.refetch()}
              >
                {t('reconnect')}
              </Button>
            </div>
          )}
          {!roster.isPending &&
            !roster.isError &&
            roster.data?.members.length === 0 && (
              <p className="text-sm text-muted-foreground">
                {t('allocationNoGrantedMembers')}
              </p>
            )}
          {mutation.isError && (
            <div className="space-y-3">
              <p role="alert" className="text-sm text-error">
                {t(allocationErrorKey(mutation.error))}
              </p>
              {editing && (
                <Button
                  variant="outline"
                  onClick={async () => {
                    const latest = await query.refetch()
                    const scheme = latest.data?.schemes.find(
                      (s) => s.id === editing.id,
                    )
                    if (scheme) {
                      setEditing(scheme)
                      setReviewGeneration((n) => n + 1)
                      mutation.reset()
                    }
                  }}
                >
                  {t('allocationReviewLatest')}
                </Button>
              )}
            </div>
          )}
        </section>
      ) : viewing !== null ? (
        <>
          <Button variant="outline" onClick={() => setViewing(null)}>
            {t('allocationBack')}
          </Button>
          <AllocationReport id={viewing} />
        </>
      ) : query.data.schemes.length === 0 ? (
        <div className="space-y-4 py-8">
          <p className="text-sm text-muted-foreground">
            {t('allocationsEmpty')}
          </p>
          <Button asChild variant="outline">
            <Link to="/groups">{t('accountGroups')}</Link>
          </Button>
        </div>
      ) : (
        <div className="overflow-hidden rounded-xl border border-border bg-card divide-y divide-border">
          {query.data.schemes.map((s) => (
            <section key={s.id} className="@container">
              <div className="flex flex-col gap-4 p-5 @3xl:flex-row @3xl:items-start @3xl:justify-between">
                <div className="min-w-0 flex-1 space-y-2">
                  <div className="flex flex-wrap items-center gap-3">
                    <h2 className="min-w-0 max-w-full break-words font-medium">
                      {s.name}
                    </h2>
                    <Status
                      kind={
                        s.enabled &&
                        s.effective_at <= query.dataUpdatedAt / 1000
                          ? 'success'
                          : 'neutral'
                      }
                    >
                      {t(
                        !s.enabled
                          ? 'disabled'
                          : s.effective_at > query.dataUpdatedAt / 1000
                            ? 'allocationWaiting'
                            : 'active',
                      )}
                    </Status>
                    <span className="text-sm text-muted-foreground">
                      {t(modeLabels[s.config.mode])}
                    </span>
                  </div>
                  <p className="break-words text-sm text-muted-foreground">
                    {s.group_name} ·{' '}
                    {s.config.period === 'durations'
                      ? t('allocationWindowSchedule', {
                          windows: s.config.windows?.length ?? 0,
                          zone: timeZone,
                        })
                      : s.config.period === 'day'
                        ? t('allocationDailySchedule', {
                            time: s.config.reset_time ?? '00:00',
                            zone: timeZone,
                          })
                        : t('allocationMonthlySchedule', {
                            day: s.config.reset_day ?? 1,
                            time: s.config.reset_time ?? '00:00',
                            zone: timeZone,
                          })}
                  </p>
                  {s.effective_at <= query.dataUpdatedAt / 1000 && (
                    <AllocationPriceWarning
                      coverage={s.price_coverage}
                      label={s.next ? t('allocationCurrentPricing') : undefined}
                      effectiveAt={s.edit_effective_at}
                    />
                  )}
                </div>
                <div className="flex shrink-0 flex-wrap items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setViewing(s.id)}
                  >
                    {t('allocationView')}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    aria-label={t('allocationEditNamed', { name: s.name })}
                    onClick={() => {
                      mutation.reset()
                      setFormPoolID(s.group_id)
                      setEditing(s)
                    }}
                  >
                    {t('allocationEdit')}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={toggle.isPending}
                    onClick={() =>
                      toggle.mutate({ id: s.id, enabled: !s.enabled })
                    }
                  >
                    {t(s.enabled ? 'allocationPause' : 'allocationResume')}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-8 text-muted-foreground hover:text-error [@media(pointer:coarse)]:size-11"
                    onClick={() => setRemoving(s)}
                    title={t('allocationDelete')}
                    aria-label={t('allocationDeleteNamed', { name: s.name })}
                  >
                    <Trash2 aria-hidden="true" className="size-4" />
                  </Button>
                </div>
              </div>
              {s.next && (
                <div className="flex flex-col gap-4 border-t border-border bg-muted/30 px-5 py-4 @3xl:flex-row @3xl:items-start @3xl:justify-between">
                  <div className="flex min-w-0 flex-1 items-start gap-3">
                    <Clock3
                      className="mt-0.5 size-4 shrink-0 text-muted-foreground"
                      aria-hidden="true"
                    />
                    <div className="min-w-0 space-y-1 break-words text-sm leading-6">
                      <p className="font-medium">
                        {t('allocationScheduled', {
                          mode: t(modeLabels[s.next.config.mode]),
                          date: date(s.next.effective_at),
                        })}
                      </p>
                      <p className="text-muted-foreground">
                        {s.next.config.period === 'durations'
                          ? t('allocationWindowSchedule', {
                              windows: s.next.config.windows?.length ?? 0,
                              zone: timeZone,
                            })
                          : s.next.config.period === 'day'
                            ? t('allocationDailySchedule', {
                                time: s.next.config.reset_time ?? '00:00',
                                zone: timeZone,
                              })
                            : t('allocationMonthlySchedule', {
                                day: s.next.config.reset_day ?? 1,
                                time: s.next.config.reset_time ?? '00:00',
                                zone: timeZone,
                              })}
                      </p>
                      <AllocationPriceWarning
                        coverage={s.next.price_coverage}
                        label={t('allocationScheduledPricing')}
                        effectiveAt={s.next.effective_at}
                        timing="scheduled"
                      />
                    </div>
                  </div>
                  {s.write_conditions && (
                    <div className="min-w-0 @3xl:max-w-md">
                      <AllocationSchedule scheme={s} onChanged={saved} />
                    </div>
                  )}
                </div>
              )}
            </section>
          ))}
        </div>
      )}
      {removing && (
        <ConfirmDelete
          title={t('allocationDeleteTitle')}
          description={t('allocationDeleteDescription', {
            name: removing.name,
          })}
          action={t('allocationDelete')}
          remove={() => deleteScheme(removing.id)}
          errorText={(error) => t(allocationDeleteErrorKey(error))}
          onClose={() => setRemoving(null)}
          onDeleted={async () => {
            await Promise.all([
              saved(),
              client.invalidateQueries({ queryKey: ['groups'] }),
            ])
          }}
        />
      )}
    </div>
  )
}
