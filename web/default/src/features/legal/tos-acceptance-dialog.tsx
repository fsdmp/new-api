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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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
import { Link } from '@tanstack/react-router'
import { getTosStatus, acceptTos } from './api'

export function useTosAcceptance() {
  const { data, isLoading } = useQuery({
    queryKey: ['tos-status'],
    queryFn: getTosStatus,
    staleTime: 30 * 1000,
  })

  return {
    needsAcceptance: data?.data?.needs_acceptance ?? false,
    isLoading,
  }
}

export function TosAcceptanceDialog({
  open,
  onAccept,
  onCancel,
}: {
  open: boolean
  onAccept: () => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [isAccepting, setIsAccepting] = useState(false)

  const handleAccept = async () => {
    setIsAccepting(true)
    try {
      await acceptTos()
      queryClient.invalidateQueries({ queryKey: ['tos-status'] })
      onAccept()
    } finally {
      setIsAccepting(false)
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={(v) => !v && onCancel()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('Accept Terms of Service')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(
              'You must accept the Terms of Service before using paid features. Please review the terms before proceeding.'
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className='text-muted-foreground text-sm'>
          <Link
            to='/terms-of-service'
            target='_blank'
            className='text-primary hover:underline'
          >
            {t('View Terms of Service')}
          </Link>
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onCancel} disabled={isAccepting}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction onClick={handleAccept} disabled={isAccepting}>
            {isAccepting ? t('Processing...') : t('Accept')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
