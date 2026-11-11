import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { catalogOptions } from '@/lib/catalog'
import { ModelPicker } from './ModelPicker'
import {
  ArrowRightLeft,
  ChevronDown,
  ExternalLink,
  LoaderCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  ccSwitchApps,
  ccSwitchEndpoint,
  ccSwitchLink,
  isImageModel,
  openCCSwitch,
  validImportSettings,
  type ImportApp,
  type ImportSettings,
} from '@/lib/ccswitch'
import type { APIKey } from '@/lib/keys'
import { ApiError } from '@/lib/request'
import { useKeySecret } from '@/hooks/use-key-secret'
import { Button } from './ui/Button'
import { Input } from './ui/Input'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from './ui/DropdownMenu'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from './ui/Dialog'

export function CCSwitchImport({
  value,
  userID,
  usable,
}: {
  value: APIKey
  userID: number
  usable: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const eligible =
    usable &&
    value.copyable &&
    value.enabled &&
    value.revoked_at === null &&
    value.group_access === 'allowed'
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="h-auto flex-col gap-1 py-1.5"
          disabled={!eligible}
          aria-label={t('ccSwitchImportNamed', { name: value.name })}
          title={t(
            !value.copyable && value.revoked_at === null
              ? 'keyLegacyCopy'
              : eligible
                ? 'ccSwitchImport'
                : 'ccSwitchKeyUnavailable',
          )}
        >
          <ArrowRightLeft aria-hidden="true" />
          <span className="text-xs font-normal">{t('ccSwitchImport')}</span>
        </Button>
      </DialogTrigger>
      {open &&
        (eligible ? (
          <ImportForm
            value={value}
            userID={userID}
            onClose={() => setOpen(false)}
          />
        ) : (
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t('ccSwitchImport')}</DialogTitle>
              <DialogDescription>
                {t('ccSwitchKeyUnavailable')}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button onClick={() => setOpen(false)}>{t('done')}</Button>
            </DialogFooter>
          </DialogContent>
        ))}
    </Dialog>
  )
}

function ImportForm({
  value,
  userID,
  onClose,
}: {
  value: APIKey
  userID: number
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(`SubLane · ${value.name}`)
  const [app, setApp] = useState<ImportApp>('codex')
  const [model, setModel] = useState('')
  const [invalid, setInvalid] = useState(false)
  const [prepared, setPrepared] = useState<string | null>(null)
  const [opened, setOpened] = useState(false)
  const [openFailed, setOpenFailed] = useState(false)
  const origin = window.location.origin
  const client = useQueryClient()
  const catalog = useQuery(
    catalogOptions(client, { kind: 'key', id: value.id }, userID),
  )
  // Image models only serve the Images endpoints and cannot drive a chat client.
  const chatModels = (catalog.data?.models ?? []).filter(
    (id) => !isImageModel(id),
  )
  const modelAllowed = chatModels.includes(model.trim())
  const { mutation, isCurrentOwner } = useKeySecret<ImportSettings>(
    userID,
    value.id,
    ({ secret, input }) => {
      setPrepared(ccSwitchLink({ ...input, secret }))
    },
  )
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (mutation.isPending || prepared) return
    const input = { origin, name, model, app }
    const valid = validImportSettings(input) && modelAllowed
    setInvalid(!valid)
    if (valid) mutation.mutate(input)
  }
  const launch = () => {
    if (!prepared || !isCurrentOwner()) return
    try {
      // A separate click preserves browser user activation after the asynchronous secret lookup.
      openCCSwitch(prepared)
      setOpened(true)
      setOpenFailed(false)
    } catch {
      setOpenFailed(true)
    }
  }
  const edit = () => {
    setPrepared(null)
    setOpened(false)
    setOpenFailed(false)
    mutation.reset()
  }
  return (
    <DialogContent
      showCloseButton={false}
      className="max-h-[calc(100dvh-2rem)] overflow-y-auto"
    >
      <DialogHeader>
        <DialogTitle>{t('ccSwitchImport')}</DialogTitle>
        <DialogDescription>{t('ccSwitchDescription')}</DialogDescription>
      </DialogHeader>
      <form onSubmit={submit} noValidate className="space-y-5">
        <div className="space-y-2">
          <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <span id="cc-switch-client-label" className="font-medium">
              {t('ccSwitchClient')}
            </span>
            <span className="text-muted-foreground">
              {t('keyGroupName', {
                name: value.group_name,
              })}
            </span>
          </div>
          <DropdownMenu modal={false}>
            <DropdownMenuTrigger asChild>
              <Button
                type="button"
                variant="outline"
                aria-labelledby="cc-switch-client-label"
                disabled={mutation.isPending || prepared !== null}
                className="w-full justify-between border-input font-normal shadow-none dark:bg-background dark:hover:bg-accent"
              >
                {t(`ccSwitchApp_${app}`)}
                <ChevronDown
                  aria-hidden="true"
                  className="text-muted-foreground"
                />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              align="start"
              className="w-[var(--radix-dropdown-menu-trigger-width)]"
            >
              <DropdownMenuRadioGroup
                value={app}
                onValueChange={(value) => setApp(value as ImportApp)}
                aria-label={t('ccSwitchClient')}
              >
                {ccSwitchApps.map((option) => (
                  <DropdownMenuRadioItem key={option} value={option}>
                    {t(`ccSwitchApp_${option}`)}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <div className="space-y-2">
          <label htmlFor="cc-switch-name" className="text-sm font-medium">
            {t('ccSwitchName')}
          </label>
          <Input
            id="cc-switch-name"
            value={name}
            maxLength={256}
            disabled={mutation.isPending || prepared !== null}
            onChange={(event) => setName(event.target.value)}
            aria-invalid={invalid}
          />
        </div>
        <div className="space-y-2">
          <ModelPicker
            id="cc-switch-model"
            value={model}
            onChange={setModel}
            models={chatModels}
            disabled={mutation.isPending || prepared !== null}
          />
          {app === 'claude' && (
            <p className="text-xs leading-5 text-muted-foreground">
              {t('ccSwitchClaudeHint')}
            </p>
          )}
          {app === 'gemini' && (
            <p className="text-xs leading-5 text-muted-foreground">
              {t('ccSwitchGeminiHint')}
            </p>
          )}
          {app === 'grokbuild' && (
            <p className="text-xs leading-5 text-muted-foreground">
              {t('ccSwitchGrokHint')}
            </p>
          )}
          {catalog.isPending && (
            <p role="status" className="text-xs text-muted-foreground">
              {t('catalogLoading')}
            </p>
          )}
          {(catalog.isError ||
            catalog.data?.refresh_failed ||
            catalog.data?.partial) && (
            <div className="space-y-2">
              <p role="status" className="text-xs text-warning">
                {t(chatModels.length ? 'catalogPartial' : 'catalogLoadFailed')}
              </p>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={catalog.isFetching}
                onClick={() => catalog.refetch()}
              >
                {t('catalogReload')}
              </Button>
            </div>
          )}
          {catalog.data?.refreshing && (
            <p role="status" className="text-xs text-muted-foreground">
              {t('catalogRefreshing')}
            </p>
          )}
          {catalog.data &&
            !catalog.data.refreshing &&
            !catalog.isError &&
            !chatModels.length && (
              <p className="text-xs text-muted-foreground">
                {t('catalogEmptyGroup')}
              </p>
            )}
        </div>
        <dl className="space-y-3 text-sm">
          <div>
            <dt className="text-muted-foreground">{t('clientEndpoint')}</dt>
            <dd className="mt-1 break-all font-mono text-xs">
              {ccSwitchEndpoint(origin, app)}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">{t('apiKey')}</dt>
            <dd className="mt-1 break-words">
              {value.name}{' '}
              <code className="text-xs text-muted-foreground">
                {value.prefix}…
              </code>
            </dd>
          </div>
        </dl>
        {invalid && (
          <p role="alert" className="text-sm text-error">
            {t(
              modelAllowed
                ? 'ccSwitchInputInvalid'
                : 'catalogSelectionRequired',
            )}
          </p>
        )}
        {mutation.isError && (
          <p role="alert" className="text-sm text-error">
            {t(
              mutation.error instanceof ApiError &&
                mutation.error.code === 'api_key_not_copyable'
                ? 'keyLegacyCopy'
                : mutation.error instanceof ApiError &&
                    mutation.error.code === 'api_key_revoked'
                  ? 'keyAlreadyRevoked'
                  : 'keyRetrieveFailed',
            )}
          </p>
        )}
        {openFailed && (
          <p role="alert" className="text-sm text-error">
            {t('ccSwitchOpenFailed')}
          </p>
        )}
        <p
          className="text-xs leading-5 text-muted-foreground"
          role={prepared ? 'status' : undefined}
        >
          {t(
            opened
              ? 'ccSwitchOpened'
              : prepared
                ? 'ccSwitchReady'
                : 'ccSwitchPrivacy',
          )}
        </p>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>
            {t(opened ? 'done' : 'cancel')}
          </Button>
          {prepared ? (
            <>
              <Button type="button" variant="ghost" onClick={edit}>
                {t('ccSwitchEdit')}
              </Button>
              <Button type="button" onClick={launch}>
                <ExternalLink aria-hidden="true" />
                {t('ccSwitchOpen')}
              </Button>
            </>
          ) : (
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending && (
                <LoaderCircle
                  aria-hidden="true"
                  className="motion-safe:animate-spin"
                />
              )}
              {t(mutation.isPending ? 'retrievingKey' : 'ccSwitchPrepare')}
            </Button>
          )}
        </DialogFooter>
      </form>
    </DialogContent>
  )
}
