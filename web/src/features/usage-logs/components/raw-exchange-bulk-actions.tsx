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
import type { Table } from '@tanstack/react-table'
import { Loader2, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTableBulkActions } from '@/components/data-table'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'

import type { UsageLog } from '../data/schema'
import { formatRawExchangeBytes } from '../lib/raw-exchange-format'
import { deleteRawExchangeBatch } from '../raw-exchange-api'

interface RawExchangeBulkActionsProps<TData> {
  table: Table<TData>
}

export function RawExchangeBulkActions<TData>({
  table,
}: RawExchangeBulkActionsProps<TData>) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)
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

  const selectedRows = table.getFilteredSelectedRowModel().rows
  const requestIds = [
    ...new Set(
      selectedRows
        .map((row) => (row.original as UsageLog).raw_exchange?.request_id)
        .filter((requestId): requestId is string => Boolean(requestId))
    ),
  ]

  const runDelete = async (proofToken?: string) => {
    if (!proofToken || requestIds.length === 0) return
    setDeleting(true)
    try {
      const result = await deleteRawExchangeBatch(requestIds, proofToken)
      if (!result.success || !result.data) {
        throw new Error(
          result.message || t('Failed to delete raw exchange archives')
        )
      }
      if (result.data.pending > 0) {
        toast.info(
          t('{{completed}} completed, {{pending}} pending, {{failed}} failed', {
            completed: result.data.deleted + result.data.already_deleted,
            pending: result.data.pending,
            failed: result.data.failed,
          })
        )
      } else if (result.data.failed > 0) {
        toast.warning(
          t('{{success}} succeeded, {{failed}} failed', {
            success: result.data.deleted + result.data.already_deleted,
            failed: result.data.failed,
          })
        )
      } else {
        toast.success(
          t('Deleted {{count}} archives and freed {{size}}.', {
            count: result.data.deleted,
            size: formatRawExchangeBytes(result.data.total_bytes_freed),
          })
        )
      }
      table.resetRowSelection()
      await queryClient.invalidateQueries({ queryKey: ['logs'] })
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to delete raw exchange archives')
      )
    } finally {
      setDeleting(false)
    }
  }

  return (
    <>
      <DataTableBulkActions
        table={table}
        entityName={t('Archive')}
        entityNamePlural={t('Archives')}
      >
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type='button'
                variant='destructive'
                size='icon'
                className='size-8'
                aria-label={t('Delete selected raw exchanges')}
                disabled={deleting || requestIds.length === 0}
                onClick={() => setConfirmOpen(true)}
              />
            }
          >
            {deleting ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Trash2 className='size-4' />
            )}
          </TooltipTrigger>
          <TooltipContent>{t('Delete selected raw exchanges')}</TooltipContent>
        </Tooltip>
      </DataTableBulkActions>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Delete selected raw exchanges?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'The selected request and response archives will be permanently removed from private storage.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleting || requestIds.length === 0}
              onClick={() => {
                setConfirmOpen(false)
                void startVerification(runDelete, {
                  scope: 'raw_exchange.delete',
                  preferredMethod: 'passkey',
                  title: t('Security verification'),
                  description: t(
                    'Use Passkey or 2FA to confirm your identity before deleting archived request data.'
                  ),
                })
              }}
            >
              {t('Delete {{count}} archives', { count: requestIds.length })}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

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
    </>
  )
}
