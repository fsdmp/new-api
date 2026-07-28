/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useCallback, useEffect, useRef, useState } from 'react'
import { Loader2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  getWeChatMpQrCode,
  getWeChatMpStatus,
} from '@/features/auth/api'
import { useAuthRedirect } from '@/features/auth/hooks/use-auth-redirect'

type ScanStatus =
  | 'loading'
  | 'pending'
  | 'logged_in'
  | 'expired'
  | 'error'

type WeChatMpDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  redirectTo?: string
}

const POLL_INTERVAL_MS = 2000

export function WeChatMpDialog({
  open,
  onOpenChange,
  redirectTo,
}: WeChatMpDialogProps) {
  const { t } = useTranslation()
  const { handleLoginSuccess } = useAuthRedirect()

  const [status, setStatus] = useState<ScanStatus>('loading')
  const [qrUrl, setQrUrl] = useState('')
  const [errorMessage, setErrorMessage] = useState('')

  const sceneIdRef = useRef<string>('')
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const expireTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // 防止并发轮询与重复登录处理
  const isHandlingLoginRef = useRef(false)
  const isFetchingQrRef = useRef(false)

  const clearTimers = useCallback(() => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current)
      pollTimerRef.current = null
    }
    if (expireTimerRef.current) {
      clearTimeout(expireTimerRef.current)
      expireTimerRef.current = null
    }
  }, [])

  const stopPolling = useCallback(() => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current)
      pollTimerRef.current = null
    }
  }, [])

  const poll = useCallback(async () => {
    const sceneId = sceneIdRef.current
    if (!sceneId) return
    try {
      const res = await getWeChatMpStatus(sceneId)
      const data = res.data
      if (!data) {
        return
      }
      if (data.status === 'logged_in') {
        if (isHandlingLoginRef.current) return
        isHandlingLoginRef.current = true
        stopPolling()
        try {
          await handleLoginSuccess(
            data as { id?: number } | null,
            redirectTo
          )
          toast.success(t('Signed in via WeChat Scan'))
          onOpenChange(false)
        } catch {
          toast.error(t('Login failed'))
          setStatus('error')
          setErrorMessage(t('Login failed'))
        } finally {
          isHandlingLoginRef.current = false
        }
        return
      }
      if (data.status === 'expired' || data.status === 'failed') {
        stopPolling()
        setStatus('expired')
        return
      }
      // pending / scanned 都继续轮询
      setStatus((prev) => (prev === 'pending' ? prev : 'pending'))
    } catch {
      // 网络错误不立即终止轮询，让用户看到 pending 状态继续等待
    }
  }, [handleLoginSuccess, onOpenChange, redirectTo, stopPolling, t])

  const fetchQrCode = useCallback(async () => {
    if (isFetchingQrRef.current) return
    isFetchingQrRef.current = true
    clearTimers()
    setStatus('loading')
    setQrUrl('')
    setErrorMessage('')
    sceneIdRef.current = ''

    try {
      const aff =
        typeof window !== 'undefined'
          ? (localStorage.getItem('aff') ?? '')
          : ''
      const res = await getWeChatMpQrCode(aff || undefined)
      if (!res?.success || !res.data) {
        setStatus('error')
        setErrorMessage(res?.message || t('Failed to generate QR code'))
        return
      }
      const { scene_id, qr_url, expire_seconds } = res.data
      sceneIdRef.current = scene_id
      setQrUrl(qr_url)
      setStatus('pending')

      // 启动轮询
      pollTimerRef.current = setInterval(poll, POLL_INTERVAL_MS)
      // 立即拉一次，缩短首次响应时间
      void poll()

      // 过期定时器（略提前 2s 触发，避免边界）
      const ttl = Math.max((expire_seconds - 2) * 1000, 10 * 1000)
      expireTimerRef.current = setTimeout(() => {
        stopPolling()
        setStatus('expired')
      }, ttl)
    } catch (err) {
      setStatus('error')
      const msg =
        err instanceof Error
          ? err.message
          : t('Failed to generate QR code')
      setErrorMessage(msg)
    } finally {
      isFetchingQrRef.current = false
    }
  }, [clearTimers, poll, stopPolling, t])

  useEffect(() => {
    if (open) {
      void fetchQrCode()
    } else {
      clearTimers()
      sceneIdRef.current = ''
      setStatus('loading')
      setQrUrl('')
      setErrorMessage('')
      isHandlingLoginRef.current = false
      isFetchingQrRef.current = false
    }
    return () => {
      // 卸载或 open 切换时清理
      if (!open) return
      clearTimers()
    }
  }, [open, fetchQrCode, clearTimers])

  // 组件卸载时彻底清理
  useEffect(() => {
    return () => {
      clearTimers()
    }
  }, [clearTimers])

  const handleRefresh = () => {
    void fetchQrCode()
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-w-sm'>
        <DialogHeader className='text-left'>
          <DialogTitle>{t('WeChat Scan Login')}</DialogTitle>
          <DialogDescription>
            {t('Scan the QR code with WeChat to sign in')}
          </DialogDescription>
        </DialogHeader>

        <div className='flex min-h-[16rem] flex-col items-center justify-center gap-4'>
          {status === 'loading' && (
            <>
              <Loader2 className='text-muted-foreground h-8 w-8 animate-spin' />
              <p className='text-muted-foreground text-sm'>
                {t('Generating QR code...')}
              </p>
            </>
          )}

          {status === 'pending' && qrUrl && (
            <>
              <img
                src={qrUrl}
                alt={t('WeChat login QR code')}
                className='h-48 w-48 rounded-md border object-contain'
              />
              <p className='text-muted-foreground text-sm'>
                {t('Waiting for scan...')}
              </p>
            </>
          )}

          {status === 'expired' && (
            <>
              <p className='text-muted-foreground text-sm'>
                {t('QR code expired')}
              </p>
              <Button
                type='button'
                variant='outline'
                onClick={handleRefresh}
                className='gap-2'
              >
                <RefreshCw className='h-4 w-4' />
                {t('Refresh QR Code')}
              </Button>
            </>
          )}

          {status === 'error' && (
            <>
              <p className='text-muted-foreground text-sm'>
                {errorMessage || t('Failed to generate QR code')}
              </p>
              <Button
                type='button'
                variant='outline'
                onClick={handleRefresh}
                className='gap-2'
              >
                <RefreshCw className='h-4 w-4' />
                {t('Retry')}
              </Button>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
