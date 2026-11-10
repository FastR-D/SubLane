import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Scale } from 'lucide-react'
import { Link } from '@tanstack/react-router'
import { accountOptions } from '@/lib/accounts'
import { channelOptions } from '@/lib/channels'
import { schedulingOptions } from '@/lib/groups'
import { authKey, type AuthState } from '@/lib/auth'
import { useTimeZone } from '@/lib/timezone'
import { Button } from './ui/Button'
import { Input } from './ui/Input'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from './ui/Dialog'

export function GroupScheduling({
  id,
  name,
  disabled = false,
}: {
  id: number
  name: string
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          disabled={disabled}
          aria-label={t('groupSchedulingFor', { name })}
        >
          <Scale aria-hidden="true" />
          {t('groupScheduling')}
        </Button>
      </DialogTrigger>
      {open && <SchedulingContent id={id} name={name} />}
    </Dialog>
  )
}
function SchedulingContent({ id, name }: { id: number; name: string }) {
  const { t, i18n } = useTranslation()
  const timeZone = useTimeZone()
  const client = useQueryClient()
  const admin = client.getQueryData<AuthState>(authKey)?.user?.role === 'admin'
  const [draft, setDraft] = useState('')
  const [model, setModel] = useState('')
  const query = useQuery({ ...schedulingOptions(id, model), enabled: admin })
  const accounts = useQuery({ ...accountOptions, enabled: admin })
  const channels = useQuery({ ...channelOptions, enabled: admin })
  const resources = [
    ...(accounts.data?.accounts ?? []),
    ...(channels.data?.channels ?? []),
  ]
  const format = (n: number) =>
    new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 2 }).format(n)
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (draft.trim() === model) await query.refetch()
    else setModel(draft.trim())
  }
  return (
    <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto">
      <DialogHeader>
        <DialogTitle>{t('groupSchedulingFor', { name })}</DialogTitle>
        <DialogDescription>{t('groupSchedulingDescription')}</DialogDescription>
      </DialogHeader>
      <Button variant="outline" asChild>
        <Link to="/groups/scheduling" search={{ group: id, model }}>
          {t('schedulingDashboard')}
        </Link>
      </Button>
      <form onSubmit={submit} className="flex flex-wrap items-end gap-2">
        <div className="min-w-0 flex-1 space-y-2">
          <label htmlFor={`scheduling-model-${id}`} className="text-sm">
            {t('groupSchedulingModel')}
          </label>
          <Input
            id={`scheduling-model-${id}`}
            maxLength={160}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
          />
        </div>
        <Button type="submit" variant="outline" disabled={query.isFetching}>
          {t('refresh')}
        </Button>
      </form>
      {query.isFetching ? (
        <p role="status" className="text-sm text-muted-foreground">
          {t('loadingGroups')}
        </p>
      ) : query.isError ? (
        <p role="alert" className="text-sm text-error">
          {t('groupSchedulingFailed')}
        </p>
      ) : (
        query.data && (
          <>
            <p className="text-xs leading-5 text-muted-foreground">
              {t(
                query.data.dynamic
                  ? 'groupSchedulingDynamic'
                  : 'groupSchedulingStatic',
              )}
            </p>
            {query.data.weights.length === 0 ? (
              <p role="status" className="text-sm text-muted-foreground">
                {t('groupSchedulingEmpty')}
              </p>
            ) : (
              <div className="divide-y divide-border">
                {query.data.weights.map((w) => (
                  <section key={w.account_id} className="space-y-2 py-3">
                    <div className="flex flex-wrap justify-between gap-2 text-sm">
                      <span className="min-w-0 break-words font-medium">
                        {resources.find((r) => r.id === w.account_id)?.name ??
                          t('groupSchedulingResource')}
                      </span>
                      <span className="tabular-nums">
                        {t('groupSchedulingShare', {
                          share: format(w.target_share * 100),
                        })}
                      </span>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      {t('groupSchedulingWeight', {
                        weight: format(w.effective_weight),
                        boost: w.preference_boost,
                      })}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {t('availabilityConcurrency', {
                        active: w.in_flight,
                        limit: w.max_concurrency,
                      })}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {t('groupSchedulingFactors', {
                        quota: format(w.quota_factor),
                        load: format(w.load_factor),
                      })}
                    </p>
                    {w.reset && (
                      <p className="text-xs text-muted-foreground">
                        {t('schedulingResetFactor', {
                          factor: format(w.reset.factor),
                        })}
                      </p>
                    )}
                    {w.quota_state !== 'available' && (
                      <p className="text-xs text-muted-foreground">
                        {t(
                          w.quota_state === 'unsupported'
                            ? 'groupSchedulingUnsupported'
                            : 'groupSchedulingFallback',
                        )}
                      </p>
                    )}
                    {w.quota_updated_at !== null && (
                      <p className="text-xs text-muted-foreground">
                        {t('availabilityObserved', {
                          date: new Intl.DateTimeFormat(i18n.language, {
                            dateStyle: 'medium',
                            timeStyle: 'short',
                            timeZone,
                          }).format(new Date(w.quota_updated_at * 1000)),
                        })}
                      </p>
                    )}
                  </section>
                ))}
              </div>
            )}
          </>
        )
      )}
    </DialogContent>
  )
}
