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
import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'
import { FormDescription, FormLabel } from '@/components/ui/form'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { ExcelModelEntry } from '../types'
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react'

function parseModels(json: string): ExcelModelEntry[] {
  if (!json || json === '[]' || json === 'null') return []
  try {
    const parsed = JSON.parse(json)
    if (!Array.isArray(parsed)) return []
    return parsed
  } catch {
    return []
  }
}

type ModelListSectionProps = {
  defaultValues: { 'excel_model_list.models': string }
}

export function ModelListSection({ defaultValues }: ModelListSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const [models, setModels] = useState<ExcelModelEntry[]>(() =>
    parseModels(defaultValues['excel_model_list.models'])
  )
  const [isSaving, setIsSaving] = useState(false)

  // Sync from external defaults when they change (e.g. after save + refetch)
  useEffect(() => {
    setModels(parseModels(defaultValues['excel_model_list.models']))
  }, [defaultValues['excel_model_list.models']])

  const addModel = useCallback(() => {
    setModels((prev) => [
      ...prev,
      { id: '', display_name: '', target_model: '', enabled: true },
    ])
  }, [])

  const removeModel = useCallback((index: number) => {
    setModels((prev) => {
      const next = [...prev]
      next.splice(index, 1)
      return next
    })
  }, [])

  const moveUp = useCallback((index: number) => {
    if (index === 0) return
    setModels((prev) => {
      const next = [...prev]
      ;[next[index - 1], next[index]] = [next[index], next[index - 1]]
      return next
    })
  }, [])

  const moveDown = useCallback((index: number) => {
    setModels((prev) => {
      if (index >= prev.length - 1) return prev
      const next = [...prev]
      ;[next[index], next[index + 1]] = [next[index + 1], next[index]]
      return next
    })
  }, [])

  const updateField = useCallback(
    (index: number, field: keyof ExcelModelEntry, value: string | boolean) => {
      setModels((prev) => {
        const next = [...prev]
        next[index] = { ...next[index], [field]: value }
        return next
      })
    },
    []
  )

  const onSave = useCallback(async () => {
    setIsSaving(true)
    try {
      await updateOption.mutateAsync({
        key: 'excel_model_list.models',
        value: JSON.stringify(models),
      })
    } finally {
      setIsSaving(false)
    }
  }, [updateOption, models])

  return (
    <SettingsSection title={t('Model List')}>
      <SettingsForm onSubmit={onSave}>
        <SettingsPageFormActions
          onSave={onSave}
          isSaving={isSaving}
        />

        <FormDescription className='mb-4'>
          {t(
            'Configure the models returned by the Excel models API. Drag to reorder. Leave empty to use default models from environment variables.'
          )}
        </FormDescription>

        {models.length === 0 && (
          <p className='text-sm text-muted-foreground py-2'>
            {t(
              'No models configured. Add a model or leave empty to use defaults.'
            )}
          </p>
        )}

        <div className='space-y-3'>
          {models.map((model, index) => (
            <div
              key={index}
              className='rounded-lg border p-4 space-y-3'
            >
              <div className='flex items-start justify-between gap-2'>
                <span className='text-sm font-medium text-muted-foreground'>
                  #{index + 1}
                </span>
                <div className='flex items-center gap-1'>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='h-7 w-7'
                    onClick={() => moveUp(index)}
                    disabled={index === 0}
                  >
                    <ArrowUp className='h-4 w-4' />
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='h-7 w-7'
                    onClick={() => moveDown(index)}
                    disabled={index === models.length - 1}
                  >
                    <ArrowDown className='h-4 w-4' />
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='h-7 w-7 text-destructive'
                    onClick={() => removeModel(index)}
                  >
                    <Trash2 className='h-4 w-4' />
                  </Button>
                </div>
              </div>

              <div className='grid grid-cols-1 md:grid-cols-3 gap-3'>
                <div className='space-y-1'>
                  <FormLabel className='text-xs'>
                    {t('Model ID')}
                  </FormLabel>
                  <Input
                    value={model.id}
                    onChange={(e) =>
                      updateField(index, 'id', e.target.value)
                    }
                    placeholder='e.g. claude-sonnet-4-6'
                  />
                </div>
                <div className='space-y-1'>
                  <FormLabel className='text-xs'>
                    {t('Display Name')}
                  </FormLabel>
                  <Input
                    value={model.display_name}
                    onChange={(e) =>
                      updateField(index, 'display_name', e.target.value)
                    }
                    placeholder='e.g. Claude Sonnet'
                  />
                </div>
                <div className='space-y-1'>
                  <FormLabel className='text-xs'>
                    {t('Target Model')}
                  </FormLabel>
                  <Input
                    value={model.target_model}
                    onChange={(e) =>
                      updateField(index, 'target_model', e.target.value)
                    }
                    placeholder={t(
                      'Leave empty to use the model ID as-is'
                    )}
                  />
                </div>
              </div>

              <div className='flex items-center gap-2'>
                <Switch
                  checked={model.enabled}
                  onCheckedChange={(checked) =>
                    updateField(index, 'enabled', checked)
                  }
                />
                <FormLabel className='text-xs cursor-pointer'>
                  {t('Enabled')}
                </FormLabel>
              </div>
            </div>
          ))}
        </div>

        <Button
          type='button'
          variant='outline'
          size='sm'
          className='mt-3'
          onClick={addModel}
        >
          <Plus className='h-4 w-4 mr-1' />
          {t('Add Model')}
        </Button>
      </SettingsForm>
    </SettingsSection>
  )
}
