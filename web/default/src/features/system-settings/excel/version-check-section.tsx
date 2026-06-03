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
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Textarea } from '@/components/ui/textarea'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

const versionCheckSchema = z.object({
  'excel_version_check.minimum_versions': z.string(),
})

type VersionCheckFormValues = z.infer<typeof versionCheckSchema>

type VersionCheckSectionProps = {
  defaultValues: VersionCheckFormValues
}

export function VersionCheckSection({
  defaultValues,
}: VersionCheckSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm({
    resolver: zodResolver(versionCheckSchema),
    defaultValues,
  })

  useResetForm(form, defaultValues)

  const onSubmit = async (data: VersionCheckFormValues) => {
    const updates = Object.entries(data).filter(
      ([key, value]) =>
        value !== defaultValues[key as keyof VersionCheckFormValues]
    )

    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value })
    }
  }

  return (
    <SettingsSection title={t('Version Check')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <FormField
            control={form.control}
            name='excel_version_check.minimum_versions'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Minimum Versions')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={4}
                    placeholder='{"excel-plugin":"1.2.0","ai-sdk":"2.0.0"}'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'JSON map of client type to minimum version (e.g. {"excel-plugin":"1.2.0"})'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
