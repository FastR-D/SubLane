import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { Channel } from '@/lib/channels'
import { ChannelForm } from './ChannelForm'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from './ui/Dialog'

export function ChannelEditor({
  channel,
  onClose,
  onSaved,
  restoreFocus,
}: {
  channel: Channel
  onClose: () => void
  onSaved: () => Promise<void>
  restoreFocus: () => void
}) {
  const { t } = useTranslation()
  const [pending, setPending] = useState(false)
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose()
      }}
    >
      <DialogContent
        showCloseButton={false}
        className="max-h-[calc(100dvh-2rem)] overflow-y-auto"
        onInteractOutside={(event) => {
          if (pending) event.preventDefault()
        }}
        onEscapeKeyDown={(event) => {
          if (pending) event.preventDefault()
        }}
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          restoreFocus()
        }}
      >
        <DialogHeader>
          <DialogTitle>{t('editChannel')}</DialogTitle>
          <DialogDescription>{t('channelEditDescription')}</DialogDescription>
        </DialogHeader>
        <ChannelForm
          channel={channel}
          onCancel={onClose}
          onPendingChange={setPending}
          onSaved={async () => {
            await onSaved()
            onClose()
          }}
        />
      </DialogContent>
    </Dialog>
  )
}
