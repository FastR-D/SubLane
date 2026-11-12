import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  type AllocationPending,
  settleAllocation,
  allocationValue,
  parseAllocationValue,
  allocationErrorKey,
} from '@/lib/allocations'
import { Button } from './ui/Button'
import { Input } from './ui/Input'

export function AllocationSettlement({
  id,
  entry,
  onSaved,
}: {
  id: number
  entry: AllocationPending
  onSaved: () => Promise<void>
}) {
  const { t } = useTranslation()
  const [invalid, setInvalid] = useState(false)
  const [open, setOpen] = useState(false)
  const mutation = useMutation({
    mutationFn: (input: Parameters<typeof settleAllocation>[1]) =>
      settleAllocation(id, input),
    onSuccess: onSaved,
  })
  return (
    <div className="space-y-3 border-b border-border pb-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="break-all text-sm font-medium">{entry.request_id}</p>
          <p className="text-xs text-muted-foreground">
            {entry.model} · {t('allocationPending')}
          </p>
        </div>
        {!open && (
          <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
            {t('allocationCorrect')}
          </Button>
        )}
      </div>
      {open && (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            if (mutation.isPending) return
            const form = new FormData(e.currentTarget)
            const input =
              parseAllocationValue(String(form.get('input') ?? ''), 'tokens') ??
              -1
            const output =
              parseAllocationValue(
                String(form.get('output') ?? ''),
                'tokens',
              ) ?? -1
            const cached =
              parseAllocationValue(
                String(form.get('cached') ?? ''),
                'tokens',
              ) ?? -1
            // An empty image share means the request had no image input.
            const imageText = String(form.get('image_input') ?? '').trim()
            const imageInput =
              imageText === ''
                ? 0
                : (parseAllocationValue(imageText, 'tokens') ?? -1)
            const bad =
              input < entry.input ||
              output < entry.output ||
              cached < 0 ||
              imageInput < 0 ||
              cached + imageInput > input
            setInvalid(bad)
            if (!bad)
              mutation.mutate({
                request_id: entry.request_id,
                input,
                output,
                cached,
                image_input: imageInput,
              })
          }}
        >
          <fieldset
            disabled={mutation.isPending}
            className="grid gap-3 sm:grid-cols-2"
          >
            {(['input', 'output', 'cached'] as const).map((key) => (
              <div key={key} className="space-y-2">
                <label
                  htmlFor={`${entry.request_id}-${key}`}
                  className="text-sm"
                >
                  {t(
                    key === 'input'
                      ? 'allocationInputM'
                      : key === 'output'
                        ? 'allocationOutputM'
                        : 'allocationCachedM',
                  )}
                </label>
                <Input
                  id={`${entry.request_id}-${key}`}
                  name={key}
                  inputMode="decimal"
                  defaultValue={allocationValue(entry[key], 'tokens')}
                />
              </div>
            ))}
            <div className="space-y-2">
              <label
                htmlFor={`${entry.request_id}-image_input`}
                className="text-sm"
              >
                {t('allocationImageInputM')}
              </label>
              <Input
                id={`${entry.request_id}-image_input`}
                name="image_input"
                inputMode="decimal"
                placeholder="0"
                aria-describedby={`${entry.request_id}-image_input-hint`}
              />
              <p
                id={`${entry.request_id}-image_input-hint`}
                className="text-xs leading-5 text-muted-foreground"
              >
                {t('allocationImageInputHint')}
              </p>
            </div>
          </fieldset>
          {(invalid || mutation.isError) && (
            <p role="alert" className="text-sm text-error">
              {t(
                invalid
                  ? 'allocationInvalid'
                  : allocationErrorKey(mutation.error!),
              )}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={mutation.isPending}
              onClick={() => setOpen(false)}
            >
              {t('cancel')}
            </Button>
            <Button disabled={mutation.isPending}>
              {t('allocationSettle')}
            </Button>
          </div>
        </form>
      )}
    </div>
  )
}
