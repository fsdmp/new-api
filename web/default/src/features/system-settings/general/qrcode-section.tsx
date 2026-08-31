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
import { useRef, useState } from 'react'
import type { ChangeEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { deleteQRCode, getQRCodeStatus, uploadQRCode } from '../api'
import { SettingsSection } from '../components/settings-section'
import type { QRCodeName } from '../types'

const MAX_QR_IMAGE_SIZE = 2 * 1024 * 1024

type QRCodeRowProps = {
  name: QRCodeName
  title: string
  description: string
  publicUrl?: string
  uploaded: boolean | undefined
  ts: number
  uploading: boolean
  deleting: boolean
  onUpload: (name: QRCodeName, image: string) => void
  onRemove: (name: QRCodeName) => void
  removeLabel: string
}

function QRCodeRow({
  name,
  title,
  description,
  publicUrl,
  uploaded,
  ts,
  uploading,
  deleting,
  onUpload,
  onRemove,
  removeLabel,
}: QRCodeRowProps) {
  const { t } = useTranslation()
  const fileInputRef = useRef<HTMLInputElement>(null)

  const handleFileChange = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) {
      return
    }
    if (file.size > MAX_QR_IMAGE_SIZE) {
      toast.error(t('Image must be 2 MB or smaller'))
      return
    }
    const reader = new FileReader()
    reader.onload = (loadEvent) => {
      const image = loadEvent.target?.result
      if (typeof image === 'string' && image) {
        onUpload(name, image)
      }
    }
    reader.readAsDataURL(file)
  }

  // mp_group 始终有预览（未上传时接口回退内置图）；site 未上传时显示占位
  const showPreview = name === 'mp_group' || uploaded === true

  return (
    <div className='flex flex-col gap-2'>
      <div className='font-medium'>{title}</div>
      <p className='text-muted-foreground text-sm'>{description}</p>
      {publicUrl && (
        <p className='text-muted-foreground text-xs break-all'>{publicUrl}</p>
      )}
      <div className='flex items-start gap-3'>
        {showPreview ? (
          <img
            src={`/api/qrcode/${name}?t=${ts}`}
            alt={title}
            className='border-muted h-32 w-32 rounded-md border object-contain'
          />
        ) : (
          <div className='text-muted-foreground flex h-32 w-32 items-center justify-center rounded-md border border-dashed text-sm'>
            {t('Not uploaded')}
          </div>
        )}
        <input
          ref={fileInputRef}
          type='file'
          accept='image/jpeg,image/png'
          className='hidden'
          onChange={handleFileChange}
        />
        <div className='flex flex-col gap-2'>
          <Button
            type='button'
            variant='outline'
            disabled={uploading}
            onClick={() => fileInputRef.current?.click()}
          >
            {uploaded ? t('Replace image') : t('Upload image')}
          </Button>
          {uploaded && (
            <Button
              type='button'
              variant='outline'
              disabled={deleting}
              onClick={() => onRemove(name)}
            >
              {removeLabel}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

export function QrcodeSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [ts, setTs] = useState(() => Date.now())

  const statusQuery = useQuery({
    queryKey: ['qrcode-status'],
    queryFn: getQRCodeStatus,
  })

  const refresh = () => {
    setTs(Date.now())
    queryClient.invalidateQueries({ queryKey: ['qrcode-status'] })
  }

  const uploadMutation = useMutation({
    mutationFn: ({
      name,
      image,
    }: {
      name: QRCodeName
      image: string
    }) => uploadQRCode(name, { image }),
    onSuccess: (data) => {
      if (data.success) {
        toast.success(t('QR code updated'))
        refresh()
      } else {
        toast.error(data.message || t('Failed to update QR code'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to update QR code'))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (name: QRCodeName) => deleteQRCode(name),
    onSuccess: (data) => {
      if (data.success) {
        toast.success(t('QR code updated'))
        refresh()
      } else {
        toast.error(data.message || t('Failed to update QR code'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to update QR code'))
    },
  })

  const uploaded = statusQuery.data?.data
  const siteQrUrl = `${window.location.origin}/api/qrcode/site`

  return (
    <SettingsSection title={t('QR Code Management')}>
      <QRCodeRow
        name='mp_group'
        title={t('Group QR Code')}
        description={t(
          'Automatically pushed to new followers of the official account and displayed on the docs download page; falls back to the built-in image when not uploaded'
        )}
        uploaded={uploaded?.mp_group}
        ts={ts}
        uploading={uploadMutation.isPending}
        deleting={deleteMutation.isPending}
        onUpload={(name, image) => uploadMutation.mutate({ name, image })}
        onRemove={(name) => deleteMutation.mutate(name)}
        removeLabel={t('Restore default image')}
      />
      <QRCodeRow
        name='site'
        title={t('Website QR Code')}
        description={t(
          'Referenced by external sites via a fixed URL; replacing the image requires no change on their side'
        )}
        publicUrl={siteQrUrl}
        uploaded={uploaded?.site}
        ts={ts}
        uploading={uploadMutation.isPending}
        deleting={deleteMutation.isPending}
        onUpload={(name, image) => uploadMutation.mutate({ name, image })}
        onRemove={(name) => deleteMutation.mutate(name)}
        removeLabel={t('Remove image')}
      />
    </SettingsSection>
  )
}
