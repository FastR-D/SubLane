import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { LoaderCircle } from 'lucide-react'
import { Button } from './ui/Button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/Dialog'

// Destructive confirmation shared by resources that delete with one request and no extra input.
export function ConfirmDelete({
  title,
  description,
  action,
  remove,
  errorText,
  onClose,
  onDeleted,
}: {
  title: string
  description: string
  action: string
  remove: () => Promise<unknown>
  errorText: (error: Error) => string
  onClose: () => void
  onDeleted: () => Promise<void>
}) {
  const { t } = useTranslation()
  const mutation = useMutation({
    mutationFn: remove,
    onSuccess: async () => {
      await onDeleted()
      onClose()
    },
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) onClose()
      }}
    >
      <DialogContent
        className="[@media(pointer:coarse)]:[&_button]:min-h-11"
        showCloseButton={false}
        onInteractOutside={(event) => {
          if (mutation.isPending) event.preventDefault()
        }}
        onEscapeKeyDown={(event) => {
          if (mutation.isPending) event.preventDefault()
        }}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {mutation.isError && (
          <p role="alert" className="text-sm text-error">
            {errorText(mutation.error)}
          </p>
        )}
        <DialogFooter>
          <Button
            variant="outline"
            onClick={onClose}
            disabled={mutation.isPending}
          >
            {t('cancel')}
          </Button>
          <Button
            variant="destructive"
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
          >
            {mutation.isPending && (
              <LoaderCircle
                className="motion-safe:animate-spin"
                aria-hidden="true"
              />
            )}
            {action}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
