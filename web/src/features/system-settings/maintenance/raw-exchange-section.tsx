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
import { Database, Loader2, RefreshCw, ShieldAlert, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DateTimePicker } from '@/components/datetime-picker'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
import { Input } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { formatTimestampToDate } from '@/lib/format'

import {
  getRawExchangeAdminStats,
  getSystemTask,
  listSystemTasks,
  previewRawExchangeCleanup,
  startRawExchangeCleanup,
} from '../api'
import {
  SettingsControlGroup,
  SettingsFormGrid,
  SettingsFormGridItem,
} from '../components/settings-form-layout'
import { SettingsSection } from '../components/settings-section'
import type {
  RawExchangeAdminStats,
  RawExchangeCleanupFilter,
  RawExchangeCleanupPreview,
  RawExchangeCleanupTask,
  RawExchangeGCTask,
} from '../types'

const RAW_EXCHANGE_CLEANUP_TYPE = 'raw_exchange_cleanup'

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(
    units.length - 1,
    Math.floor(Math.log(bytes) / Math.log(1024))
  )
  return `${(bytes / 1024 ** index).toFixed(index === 0 ? 0 : 2)} ${units[index]}`
}

function formatSignedBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes === 0) return '0 B'
  return `${bytes > 0 ? '+' : '-'}${formatBytes(Math.abs(bytes))}`
}

function getDateDaysAgo(days: number): Date {
  const date = new Date()
  date.setDate(date.getDate() - days)
  return date
}

function statusLabel(status: string, t: (key: string) => string): string {
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
      return status
  }
}

function taskStatusLabel(
  status: RawExchangeCleanupTask['status'],
  t: (key: string) => string
): string {
  switch (status) {
    case 'pending':
      return t('Pending')
    case 'running':
      return t('Running')
    case 'succeeded':
      return t('Success')
    case 'failed':
      return t('Failed')
  }
}

function cleanupFilterIsEmpty(filter: RawExchangeCleanupFilter): boolean {
  return Object.values(filter).every(
    (value) => value === undefined || value === '' || value === 0
  )
}

export function RawExchangeSection() {
  const { t } = useTranslation()
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
  const [afterDate, setAfterDate] = useState<Date | undefined>()
  const [beforeDate, setBeforeDate] = useState<Date | undefined>(() =>
    getDateDaysAgo(30)
  )
  const [userId, setUserId] = useState('')
  const [tokenId, setTokenId] = useState('')
  const [captureMode, setCaptureMode] = useState('')
  const [outcome, setOutcome] = useState('')
  const [status, setStatus] = useState('')
  const [preview, setPreview] = useState<RawExchangeCleanupPreview | null>(null)
  const [previewToken, setPreviewToken] = useState('')
  const [previewExpiresAt, setPreviewExpiresAt] = useState(0)
  const [isPreviewing, setIsPreviewing] = useState(false)
  const [isStarting, setIsStarting] = useState(false)
  const [fullCleanupConfirmation, setFullCleanupConfirmation] = useState('')
  const [task, setTask] = useState<RawExchangeCleanupTask | null>(null)

  const [stats, setStats] = useState<RawExchangeAdminStats | null>(null)
  const [statsMeta, setStatsMeta] = useState<{
    capture_enabled: boolean
    storage_ready: boolean
    storage_error?: string
    storage_backend: string
    retention_days: number
    capacity_bytes: number
    latest_gc?: RawExchangeGCTask | null
  } | null>(null)
  const [isRefreshing, setIsRefreshing] = useState(false)

  const refreshStats = useCallback(async () => {
    setIsRefreshing(true)
    try {
      const result = await getRawExchangeAdminStats()
      if (!result.success || !result.data) {
        throw new Error(
          result.message || t('Failed to load raw exchange statistics')
        )
      }
      setStats({
        ...result.data.stats,
        statuses: result.data.stats.statuses ?? [],
        top_users: result.data.stats.top_users ?? [],
        top_tokens: result.data.stats.top_tokens ?? [],
      })
      setStatsMeta(result.data)
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to load raw exchange statistics')
      )
    } finally {
      setIsRefreshing(false)
    }
  }, [t])

  useEffect(() => {
    void refreshStats()
  }, [refreshStats])

  useEffect(() => {
    let cancelled = false
    async function loadActiveTask() {
      try {
        const result = await listSystemTasks(50)
        const activeTask = result.data?.find(
          (item) =>
            item.type === RAW_EXCHANGE_CLEANUP_TYPE &&
            (item.status === 'pending' || item.status === 'running')
        )
        if (!cancelled && activeTask) {
          const current = await getSystemTask<RawExchangeCleanupTask>(
            activeTask.task_id
          )
          if (!cancelled && current.success && current.data) {
            setTask(current.data)
          }
        }
      } catch {
        /* The stats and cleanup controls remain usable if task discovery fails. */
      }
    }
    void loadActiveTask()
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!task || (task.status !== 'pending' && task.status !== 'running')) {
      return
    }
    let cancelled = false
    const interval = window.setInterval(async () => {
      try {
        const result = await getSystemTask<RawExchangeCleanupTask>(task.task_id)
        if (cancelled || !result.success || !result.data) return
        setTask(result.data)
        if (result.data.status === 'succeeded') {
          await refreshStats()
          toast.success(
            t('Raw exchange cleanup finished. Freed {{size}}.', {
              size: formatBytes(result.data.result?.total_bytes_freed ?? 0),
            })
          )
        } else if (result.data.status === 'failed') {
          toast.error(result.data.error || t('Raw exchange cleanup failed'))
        }
      } catch {
        /* Keep polling through transient API failures. */
      }
    }, 1000)
    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [refreshStats, task, t])

  const filter = useMemo<RawExchangeCleanupFilter>(() => {
    const next: RawExchangeCleanupFilter = {}
    if (afterDate) {
      next.after_timestamp = Math.floor(afterDate.getTime() / 1000)
    }
    if (beforeDate) {
      next.before_timestamp = Math.floor(beforeDate.getTime() / 1000)
    }
    const parsedUserId = Number.parseInt(userId, 10)
    const parsedTokenId = Number.parseInt(tokenId, 10)
    if (Number.isSafeInteger(parsedUserId) && parsedUserId > 0) {
      next.user_id = parsedUserId
    }
    if (Number.isSafeInteger(parsedTokenId) && parsedTokenId > 0) {
      next.token_id = parsedTokenId
    }
    if (captureMode) next.capture_mode = captureMode as 'non_success' | 'all'
    if (outcome) next.outcome = outcome as 'success' | 'non_success'
    if (status) next.status = status
    return next
  }, [afterDate, beforeDate, captureMode, outcome, status, tokenId, userId])

  useEffect(() => {
    setPreview(null)
    setPreviewToken('')
    setPreviewExpiresAt(0)
    setFullCleanupConfirmation('')
  }, [filter])

  const isFullCleanup = cleanupFilterIsEmpty(filter)
  const activeTask = task?.status === 'pending' || task?.status === 'running'
  const taskProgress = Math.max(0, Math.min(100, task?.state?.progress ?? 0))

  const handlePreview = async () => {
    setIsPreviewing(true)
    try {
      const result = await previewRawExchangeCleanup(filter)
      if (!result.success || !result.data) {
        throw new Error(
          result.message || t('Failed to preview raw exchange cleanup')
        )
      }
      setPreview(result.data.preview)
      setPreviewToken(result.data.preview_token)
      setPreviewExpiresAt(result.data.expires_at)
    } catch (error) {
      setPreview(null)
      setPreviewToken('')
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to preview raw exchange cleanup')
      )
    } finally {
      setIsPreviewing(false)
    }
  }

  const runCleanup = async (proofToken?: string) => {
    if (!previewToken || !proofToken) return
    setIsStarting(true)
    try {
      const result = await startRawExchangeCleanup(
        previewToken,
        isFullCleanup ? fullCleanupConfirmation : undefined,
        proofToken
      )
      if (!result.success || !result.data?.task) {
        throw new Error(
          result.message || t('Failed to start raw exchange cleanup')
        )
      }
      setTask(result.data.task)
      setPreview(null)
      setPreviewToken('')
      setFullCleanupConfirmation('')
      toast.success(t('Raw exchange cleanup task started.'))
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to start raw exchange cleanup')
      )
    } finally {
      setIsStarting(false)
    }
  }

  const handleStart = () =>
    startVerification(runCleanup, {
      scope: 'raw_exchange.admin.cleanup',
      preferredMethod: 'passkey',
      title: t('Security verification'),
      description: t(
        'Use Passkey or 2FA to confirm your identity before deleting raw exchange archives.'
      ),
    })

  return (
    <SettingsSection title={t('Raw Exchange Storage')}>
      <SettingsControlGroup className='space-y-4'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div className='flex items-center gap-2'>
            <Database
              className='text-muted-foreground size-4'
              aria-hidden='true'
            />
            <span className='text-sm font-medium'>{t('Storage overview')}</span>
          </div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => void refreshStats()}
            disabled={isRefreshing}
          >
            {isRefreshing ? (
              <Loader2 className='size-3.5 animate-spin' />
            ) : (
              <RefreshCw className='size-3.5' />
            )}
            {t('Refresh')}
          </Button>
        </div>
        {statsMeta && !statsMeta.storage_ready && (
          <Alert variant='destructive'>
            <ShieldAlert aria-hidden='true' />
            <AlertDescription>
              {statsMeta.storage_error ||
                t('Raw exchange storage is unavailable.')}
            </AlertDescription>
          </Alert>
        )}
        {stats && (
          <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
            <div className='bg-muted/20 rounded-md p-3'>
              <div className='text-muted-foreground text-xs'>
                {t('Archives')}
              </div>
              <div className='mt-1 text-lg font-semibold tabular-nums'>
                {stats.total_count.toLocaleString()}
              </div>
            </div>
            <div className='bg-muted/20 rounded-md p-3'>
              <div className='text-muted-foreground text-xs'>
                {t('Committed storage')}
              </div>
              <div className='mt-1 text-lg font-semibold tabular-nums'>
                {formatBytes(stats.used_bytes)}
              </div>
              <div className='text-muted-foreground mt-0.5 text-xs'>
                {statsMeta?.capacity_bytes
                  ? t('of {{size}}', {
                      size: formatBytes(statsMeta.capacity_bytes),
                    })
                  : t('No capacity limit')}
              </div>
            </div>
            <div className='bg-muted/20 rounded-md p-3'>
              <div className='text-muted-foreground text-xs'>
                {t('Request bytes')}
              </div>
              <div className='mt-1 text-lg font-semibold tabular-nums'>
                {formatBytes(stats.request_bytes)}
              </div>
              <div className='text-muted-foreground mt-0.5 text-xs'>
                {t('Stored: {{size}}', {
                  size: formatBytes(stats.request_stored_bytes),
                })}
              </div>
            </div>
            <div className='bg-muted/20 rounded-md p-3'>
              <div className='text-muted-foreground text-xs'>
                {t('Response bytes')}
              </div>
              <div className='mt-1 text-lg font-semibold tabular-nums'>
                {formatBytes(stats.response_bytes)}
              </div>
              <div className='text-muted-foreground mt-0.5 text-xs'>
                {t('Stored: {{size}}', {
                  size: formatBytes(stats.response_stored_bytes),
                })}
              </div>
            </div>
          </div>
        )}
        {statsMeta && (
          <div className='text-muted-foreground text-xs'>
            {t(
              'Backend: {{backend}} · Retention: {{days}} days · Capture: {{state}}',
              {
                backend: statsMeta.storage_backend,
                days: statsMeta.retention_days,
                state: statsMeta.capture_enabled ? t('enabled') : t('disabled'),
              }
            )}
          </div>
        )}
        {statsMeta?.latest_gc && (
          <div className='border-border/60 flex flex-wrap items-center justify-between gap-2 border-y py-2 text-xs'>
            <span>
              {t('Last GC: {{time}}', {
                time: formatTimestampToDate(statsMeta.latest_gc.updated_at),
              })}{' '}
              · {taskStatusLabel(statsMeta.latest_gc.status, t)}
            </span>
            <span className='text-muted-foreground tabular-nums'>
              {t(
                'Missing: {{missing}} · Orphans deleted: {{orphans}} · Failures: {{failed}}',
                {
                  missing: statsMeta.latest_gc.result?.missing_marked ?? 0,
                  orphans:
                    statsMeta.latest_gc.result?.orphan_objects_deleted ?? 0,
                  failed: statsMeta.latest_gc.result?.reconcile_failed ?? 0,
                }
              )}{' '}
              · {t('Tombstones purged')}:{' '}
              {statsMeta.latest_gc.result?.tombstones_purged ?? 0} ·{' '}
              {t('Usage adjustment')}:{' '}
              {formatSignedBytes(
                statsMeta.latest_gc.result?.usage_adjustment_bytes ?? 0
              )}
            </span>
          </div>
        )}
        {stats?.statuses && stats.statuses.length > 0 && (
          <div className='space-y-2'>
            <h4 className='text-sm font-medium'>{t('Archive status')}</h4>
            <div className='grid gap-2 sm:grid-cols-2 lg:grid-cols-3'>
              {stats.statuses.map((item) => (
                <div
                  key={item.status}
                  className='border-border/60 flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm'
                >
                  <span>{statusLabel(item.status, t)}</span>
                  <span className='text-muted-foreground tabular-nums'>
                    {item.count.toLocaleString()} ·{' '}
                    {formatBytes(item.stored_bytes)}
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}
        {stats &&
          stats.top_users.length === 0 &&
          stats.top_tokens.length === 0 && (
            <div className='border-border/60 text-muted-foreground rounded-md border px-3 py-4 text-center text-sm'>
              {t('No usage data yet.')}
            </div>
          )}
        {stats &&
          (stats.top_users.length > 0 || stats.top_tokens.length > 0) && (
            <div className='grid gap-4 lg:grid-cols-2'>
              <div className='space-y-2'>
                <h4 className='text-sm font-medium'>
                  {t('Top users by archive storage')}
                </h4>
                {stats.top_users.map((item) => (
                  <div
                    key={item.scope_id}
                    className='border-border/60 flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm'
                  >
                    <span>
                      {t('User')} #{item.scope_id}
                    </span>
                    <span className='text-muted-foreground tabular-nums'>
                      {item.count.toLocaleString()} ·{' '}
                      {formatBytes(item.stored_bytes)}
                    </span>
                  </div>
                ))}
              </div>
              <div className='space-y-2'>
                <h4 className='text-sm font-medium'>
                  {t('Top API keys by archive storage')}
                </h4>
                {stats.top_tokens.map((item) => (
                  <div
                    key={item.scope_id}
                    className='border-border/60 flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm'
                  >
                    <span>
                      {t('API Key')} #{item.scope_id}
                    </span>
                    <span className='text-muted-foreground tabular-nums'>
                      {item.count.toLocaleString()} ·{' '}
                      {formatBytes(item.stored_bytes)}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}
      </SettingsControlGroup>

      <SettingsControlGroup className='space-y-4'>
        <div>
          <h4 className='text-sm font-medium'>{t('Cleanup archives')}</h4>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              'Preview the matching metadata before starting an asynchronous deletion task.'
            )}
          </p>
        </div>
        <SettingsFormGrid>
          <SettingsFormGridItem>
            <div className='space-y-1.5'>
              <label className='text-sm font-medium'>
                {t('Created after')}
              </label>
              <DateTimePicker value={afterDate} onChange={setAfterDate} />
              <button
                type='button'
                className='text-muted-foreground hover:text-foreground text-xs underline underline-offset-2'
                onClick={() => setAfterDate(undefined)}
              >
                {t('Clear start date')}
              </button>
            </div>
          </SettingsFormGridItem>
          <SettingsFormGridItem>
            <div className='space-y-1.5'>
              <label className='text-sm font-medium'>
                {t('Created before')}
              </label>
              <DateTimePicker value={beforeDate} onChange={setBeforeDate} />
              <button
                type='button'
                className='text-muted-foreground hover:text-foreground text-xs underline underline-offset-2'
                onClick={() => setBeforeDate(undefined)}
              >
                {t('Clear date for all archives')}
              </button>
            </div>
          </SettingsFormGridItem>
          <SettingsFormGridItem>
            <label className='text-sm font-medium'>{t('User ID')}</label>
            <Input
              type='number'
              min='1'
              value={userId}
              onChange={(event) => setUserId(event.target.value)}
              placeholder={t('Optional')}
            />
          </SettingsFormGridItem>
          <SettingsFormGridItem>
            <label className='text-sm font-medium'>{t('Token ID')}</label>
            <Input
              type='number'
              min='1'
              value={tokenId}
              onChange={(event) => setTokenId(event.target.value)}
              placeholder={t('Optional')}
            />
          </SettingsFormGridItem>
          <SettingsFormGridItem>
            <label className='text-sm font-medium'>{t('Capture mode')}</label>
            <Select
              items={[
                { value: 'any', label: t('Any') },
                { value: 'non_success', label: t('Non-success') },
                { value: 'all', label: t('All requests') },
              ]}
              value={captureMode || 'any'}
              onValueChange={(value) =>
                value !== null && setCaptureMode(value === 'any' ? '' : value)
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='any'>{t('Any')}</SelectItem>
                <SelectItem value='non_success'>{t('Non-success')}</SelectItem>
                <SelectItem value='all'>{t('All requests')}</SelectItem>
              </SelectContent>
            </Select>
          </SettingsFormGridItem>
          <SettingsFormGridItem>
            <label className='text-sm font-medium'>{t('Outcome')}</label>
            <Select
              items={[
                { value: 'any', label: t('Any') },
                { value: 'success', label: t('Success') },
                { value: 'non_success', label: t('Non-success') },
              ]}
              value={outcome || 'any'}
              onValueChange={(value) =>
                value !== null && setOutcome(value === 'any' ? '' : value)
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='any'>{t('Any')}</SelectItem>
                <SelectItem value='success'>{t('Success')}</SelectItem>
                <SelectItem value='non_success'>{t('Non-success')}</SelectItem>
              </SelectContent>
            </Select>
          </SettingsFormGridItem>
          <SettingsFormGridItem>
            <label className='text-sm font-medium'>{t('Status')}</label>
            <Select
              items={[
                { value: 'any', label: t('Any') },
                { value: 'stored', label: t('Stored') },
                { value: 'pending_commit', label: t('Pending') },
                { value: 'capture_failed', label: t('Capture failed') },
                { value: 'skipped_capacity', label: t('Capacity skipped') },
                { value: 'skipped_too_large', label: t('Too large') },
                { value: 'delete_pending', label: t('Deleting') },
                { value: 'delete_failed', label: t('Delete failed') },
                { value: 'deleted', label: t('Archive deleted') },
                { value: 'missing', label: t('Missing') },
              ]}
              value={status || 'any'}
              onValueChange={(value) =>
                value !== null && setStatus(value === 'any' ? '' : value)
              }
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='any'>{t('Any')}</SelectItem>
                <SelectItem value='stored'>{t('Stored')}</SelectItem>
                <SelectItem value='pending_commit'>{t('Pending')}</SelectItem>
                <SelectItem value='capture_failed'>
                  {t('Capture failed')}
                </SelectItem>
                <SelectItem value='skipped_capacity'>
                  {t('Capacity skipped')}
                </SelectItem>
                <SelectItem value='skipped_too_large'>
                  {t('Too large')}
                </SelectItem>
                <SelectItem value='delete_pending'>{t('Deleting')}</SelectItem>
                <SelectItem value='delete_failed'>
                  {t('Delete failed')}
                </SelectItem>
                <SelectItem value='deleted'>{t('Archive deleted')}</SelectItem>
                <SelectItem value='missing'>{t('Missing')}</SelectItem>
              </SelectContent>
            </Select>
          </SettingsFormGridItem>
        </SettingsFormGrid>
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            type='button'
            variant='outline'
            onClick={() => void handlePreview()}
            disabled={isPreviewing || activeTask}
          >
            {isPreviewing && <Loader2 className='size-3.5 animate-spin' />}
            {t('Preview cleanup')}
          </Button>
          {isFullCleanup && (
            <span className='text-muted-foreground text-xs'>
              {t('Full cleanup requires an explicit confirmation.')}
            </span>
          )}
        </div>
        {preview && (
          <div className='border-border/60 space-y-3 rounded-md border p-3'>
            <div className='text-sm font-medium'>{t('Cleanup preview')}</div>
            <div className='grid gap-2 text-sm sm:grid-cols-3'>
              <span>{t('{{count}} archives', { count: preview.count })}</span>
              <span>
                {t('Request: {{size}}', {
                  size: formatBytes(preview.request_stored_bytes),
                })}
              </span>
              <span>
                {t('Response: {{size}}', {
                  size: formatBytes(preview.response_stored_bytes),
                })}
              </span>
            </div>
            <div className='flex flex-wrap items-center gap-2'>
              <AlertDialog>
                <AlertDialogTrigger
                  render={
                    <Button
                      type='button'
                      variant='destructive'
                      disabled={isStarting || activeTask || preview.count === 0}
                    />
                  }
                >
                  {isStarting && <Loader2 className='size-3.5 animate-spin' />}
                  <Trash2 className='size-3.5' />
                  {t('Start cleanup')}
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>
                      {t('Start raw exchange cleanup?')}
                    </AlertDialogTitle>
                    <AlertDialogDescription>
                      {isFullCleanup
                        ? t(
                            'This will delete every raw exchange archive. This action cannot be undone.'
                          )
                        : t(
                            'This will delete the archives matched by the preview. This action cannot be undone.'
                          )}
                    </AlertDialogDescription>
                    {isFullCleanup && (
                      <div className='space-y-1.5'>
                        <label className='text-sm font-medium'>
                          {t('Type DELETE ALL RAW EXCHANGES to confirm.')}
                        </label>
                        <Input
                          value={fullCleanupConfirmation}
                          onChange={(event) =>
                            setFullCleanupConfirmation(event.target.value)
                          }
                          autoComplete='off'
                          spellCheck={false}
                          placeholder='DELETE ALL RAW EXCHANGES'
                        />
                      </div>
                    )}
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
                    <AlertDialogAction
                      variant='destructive'
                      onClick={() => void handleStart()}
                      disabled={
                        isStarting ||
                        (isFullCleanup &&
                          fullCleanupConfirmation !==
                            'DELETE ALL RAW EXCHANGES')
                      }
                    >
                      {t('Confirm cleanup')}
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
              <span className='text-muted-foreground text-xs'>
                {t('Preview expires at {{time}}', {
                  time: formatTimestampToDate(previewExpiresAt),
                })}
              </span>
            </div>
          </div>
        )}
        {task && (
          <div className='border-border/60 space-y-2 rounded-md border p-3'>
            <div className='flex items-center justify-between gap-3 text-sm'>
              <span className='font-medium'>{t('Cleanup task')}</span>
              <span className='text-muted-foreground'>
                {taskStatusLabel(task.status, t)}
              </span>
            </div>
            <Progress value={taskProgress} />
            <div className='text-muted-foreground flex flex-wrap justify-between gap-2 text-xs'>
              <span>
                {t('{{processed}} of {{total}} processed', {
                  processed: task.state?.processed ?? 0,
                  total: task.state?.total ?? 0,
                })}
              </span>
              <span>{taskProgress}%</span>
            </div>
            {task.result &&
              (task.status === 'succeeded' || task.status === 'failed') && (
                <div className='border-border/60 grid gap-2 border-t pt-2 text-xs sm:grid-cols-3'>
                  <span>
                    {t('Deleted')}: {task.result.deleted.toLocaleString()}
                  </span>
                  <span>
                    {t('Failed')}: {task.result.failed.toLocaleString()}
                  </span>
                  <span>
                    {t('Total')}: {formatBytes(task.result.total_bytes_freed)}
                  </span>
                </div>
              )}
            {task.status === 'failed' && task.error && (
              <div className='text-destructive text-xs'>{task.error}</div>
            )}
          </div>
        )}
      </SettingsControlGroup>
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
    </SettingsSection>
  )
}
