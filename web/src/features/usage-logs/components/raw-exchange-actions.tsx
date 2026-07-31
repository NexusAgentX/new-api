/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/
import { useQueryClient } from '@tanstack/react-query'
import {
  Archive,
  CheckCircle2,
  FileArchive,
  FileInput,
  FileOutput,
  Loader2,
  Trash2,
  TriangleAlert,
  type LucideIcon,
} from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'

import type { RawExchangeSummary } from '../data/schema'
import { getRawExchangeActionState } from '../lib/raw-exchange-actions'
import {
  formatRawExchangeBytes,
  rawExchangeCaptureModeLabel,
  rawExchangeOutcomeLabel,
} from '../lib/raw-exchange-format'
import { deleteRawExchange, downloadRawExchange } from '../raw-exchange-api'

interface RawExchangeActionsProps {
  archive: RawExchangeSummary
  isAdmin: boolean
  compact?: boolean
  showMetadata?: boolean
}

function getStatusLabel(
  status: RawExchangeSummary['status'],
  t: (key: string) => string
): string {
  switch (status) {
    case 'stored':
      return t('Stored')
    case 'pending_commit':
      return t('Pending')
    case 'skipped_too_large':
      return t('Too large')
    case 'skipped_capacity':
      return t('Capacity skipped')
    case 'capture_failed':
      return t('Capture failed')
    case 'delete_pending':
      return t('Deleting')
    case 'delete_failed':
      return t('Delete failed')
    case 'deleted':
      return t('Archive deleted')
    case 'missing':
      return t('Missing')
    default:
      return t('Unknown')
  }
}

function getStatusVariant(
  status: RawExchangeSummary['status']
): StatusBadgeProps['variant'] {
  switch (status) {
    case 'stored':
      return 'success'
    case 'capture_failed':
    case 'delete_failed':
    case 'missing':
      return 'danger'
    case 'skipped_too_large':
    case 'skipped_capacity':
      return 'warning'
    case 'deleted':
      return 'neutral'
    default:
      return 'info'
  }
}

function getStatusIcon(status: RawExchangeSummary['status']): LucideIcon {
  switch (status) {
    case 'stored':
      return CheckCircle2
    case 'capture_failed':
    case 'delete_failed':
    case 'missing':
      return TriangleAlert
    default:
      return Archive
  }
}

function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}

export function RawExchangeActions({
  archive,
  isAdmin,
  compact = false,
  showMetadata = true,
}: RawExchangeActionsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [isDownloading, setIsDownloading] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleted, setDeleted] = useState(archive.status === 'deleted')
  const [deletionPending, setDeletionPending] = useState(
    archive.status === 'delete_pending'
  )
  const [confirmOpen, setConfirmOpen] = useState(false)
  const {
    open: verificationOpen,
    methods: verificationMethods,
    state: verificationState,
    startVerification,
    executeVerification,
    cancel: cancelVerification,
    setCode: setVerificationCode,
    switchMethod: switchVerificationMethod,
  } = useSecureVerification()

  let status: RawExchangeSummary['status'] = archive.status
  if (deletionPending) status = 'delete_pending'
  if (deleted) status = 'deleted'
  const actionState = getRawExchangeActionState({ ...archive, status })

  const runDownload = async (
    part: 'request' | 'response' | 'bundle',
    fallbackName: string,
    proofToken?: string
  ) => {
    if (!proofToken) return
    setIsDownloading(true)
    try {
      const result = await downloadRawExchange(
        archive.request_id,
        part,
        proofToken
      )
      saveBlob(result.blob, result.filename || fallbackName)
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to download raw exchange')
      )
    } finally {
      setIsDownloading(false)
    }
  }

  const requestDownload = (
    part: 'request' | 'response' | 'bundle',
    fallbackName: string
  ) =>
    startVerification(
      (proofToken) => runDownload(part, fallbackName, proofToken),
      {
        scope: 'raw_exchange.read',
        preferredMethod: 'passkey',
        title: t('Security verification'),
        description: t(
          'Use Passkey or 2FA to confirm your identity before accessing archived request data.'
        ),
      }
    )

  const runDelete = async (proofToken?: string) => {
    if (!proofToken) return
    setDeleting(true)
    try {
      const result = await deleteRawExchange(archive.request_id, proofToken)
      if (!result.success) {
        throw new Error(result.message || t('Failed to delete raw exchange'))
      }
      setConfirmOpen(false)
      if (result.data?.deletion_pending) {
        setDeletionPending(true)
        toast.success(t('Raw exchange deletion is pending'))
      } else {
        setDeleted(true)
        toast.success(t('Raw exchange deleted'))
      }
      void queryClient.invalidateQueries({ queryKey: ['logs'] })
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to delete raw exchange')
      )
    } finally {
      setDeleting(false)
    }
  }

  const actionButton = (
    label: string,
    icon: ReactNode,
    onClick: () => void,
    availability: { enabled: boolean; reason?: string },
    busy = false
  ) => (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type='button'
            variant='ghost'
            size='icon'
            className='size-7'
            aria-label={label}
            aria-description={
              availability.enabled ? undefined : t(availability.reason ?? '')
            }
            disabled={!availability.enabled || busy}
            onClick={onClick}
          />
        }
      >
        {icon}
      </TooltipTrigger>
      <TooltipContent>
        {availability.enabled || busy ? label : t(availability.reason ?? label)}
      </TooltipContent>
    </Tooltip>
  )

  return (
    <TooltipProvider>
      <div className='flex min-w-0 flex-col items-start gap-1'>
        <div
          className={
            compact
              ? 'flex min-w-0 items-center gap-1'
              : 'flex min-w-0 flex-wrap items-center gap-2'
          }
        >
          <StatusBadge
            label={getStatusLabel(status, t)}
            variant={getStatusVariant(status)}
            size='sm'
            copyable={false}
            icon={getStatusIcon(status)}
          />
          {!isAdmin && (
            <div className='flex items-center gap-0.5'>
              {actionButton(
                t('Download request'),
                isDownloading ? (
                  <Loader2 className='size-3.5 animate-spin' />
                ) : (
                  <FileInput className='size-3.5' />
                ),
                () =>
                  void requestDownload(
                    'request',
                    `${archive.request_id}-request.body`
                  ),
                actionState.request,
                isDownloading
              )}
              {actionButton(
                t('Download response'),
                isDownloading ? (
                  <Loader2 className='size-3.5 animate-spin' />
                ) : (
                  <FileOutput className='size-3.5' />
                ),
                () =>
                  void requestDownload(
                    'response',
                    `${archive.request_id}-response.body`
                  ),
                actionState.response,
                isDownloading
              )}
              {actionButton(
                t('Download bundle'),
                isDownloading ? (
                  <Loader2 className='size-3.5 animate-spin' />
                ) : (
                  <FileArchive className='size-3.5' />
                ),
                () =>
                  void requestDownload(
                    'bundle',
                    `${archive.request_id}-exchange.zip`
                  ),
                actionState.bundle,
                isDownloading
              )}
            </div>
          )}
          {!isAdmin && actionState.delete.enabled && (
            <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
              <AlertDialogTrigger
                render={
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='text-destructive hover:text-destructive size-7'
                    aria-label={t('Delete raw exchange')}
                    disabled={deleting}
                  />
                }
              >
                {deleting ? (
                  <Loader2 className='size-3.5 animate-spin' />
                ) : (
                  <Trash2 className='size-3.5' />
                )}
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>
                    {t('Delete this raw exchange?')}
                  </AlertDialogTitle>
                  <AlertDialogDescription>
                    {t(
                      'The archived request and response will be permanently removed from private storage.'
                    )}
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
                  <AlertDialogAction
                    variant='destructive'
                    onClick={() =>
                      void startVerification(runDelete, {
                        scope: 'raw_exchange.delete',
                        preferredMethod: 'passkey',
                        title: t('Security verification'),
                        description: t(
                          'Use Passkey or 2FA to confirm your identity before deleting archived request data.'
                        ),
                      })
                    }
                    disabled={deleting}
                  >
                    {t('Delete')}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          )}
          {!isAdmin &&
            !actionState.delete.enabled &&
            actionButton(
              t('Delete raw exchange'),
              <Trash2 className='size-3.5' />,
              () => undefined,
              actionState.delete
            )}
        </div>
        {showMetadata && (
          <>
            <div className='text-muted-foreground flex min-w-0 flex-wrap items-center gap-x-1 text-[11px] leading-tight'>
              <span>
                {t('Capture mode')}:{' '}
                {rawExchangeCaptureModeLabel(archive.capture_mode, t)}
              </span>
              <span aria-hidden='true'>·</span>
              <span>
                {t('Outcome')}: {rawExchangeOutcomeLabel(archive.outcome, t)}
              </span>
            </div>
            <div className='text-muted-foreground flex min-w-0 flex-wrap items-center gap-x-1 text-[11px] leading-tight tabular-nums'>
              <span>
                {t('Request: {{size}}', {
                  size: formatRawExchangeBytes(archive.request_bytes),
                })}
              </span>
              <span aria-hidden='true'>·</span>
              <span>
                {t('Response: {{size}}', {
                  size: formatRawExchangeBytes(archive.response_bytes),
                })}
              </span>
              {archive.response_available && !archive.response_complete && (
                <>
                  <span aria-hidden='true'>·</span>
                  <span>{t('Incomplete')}</span>
                </>
              )}
            </div>
          </>
        )}
      </div>
      <SecureVerificationDialog
        open={verificationOpen}
        onOpenChange={(open) => {
          if (!open) cancelVerification()
        }}
        methods={verificationMethods}
        state={verificationState}
        onVerify={async (method, code) => {
          await executeVerification(method, code)
        }}
        onCancel={cancelVerification}
        onCodeChange={setVerificationCode}
        onMethodChange={switchVerificationMethod}
      />
    </TooltipProvider>
  )
}
