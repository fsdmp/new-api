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
import type { ExcelSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { TmpKeySection } from './tmp-key-section'
import { VersionCheckSection } from './version-check-section'
import { ModelListSection } from './model-list-section'

const EXCEL_SECTIONS = [
  {
    id: 'tmp-key',
    titleKey: 'Temporary Key',
    build: (settings: ExcelSettings) => (
      <TmpKeySection
        defaultValues={{
          'excel_tmp_key.enabled': settings['excel_tmp_key.enabled'],
          'excel_tmp_key.account': settings['excel_tmp_key.account'],
          'excel_tmp_key.expire_days': settings['excel_tmp_key.expire_days'],
          'excel_tmp_key.quota': settings['excel_tmp_key.quota'],
        }}
      />
    ),
  },
  {
    id: 'model-list',
    titleKey: 'Model List',
    build: (settings: ExcelSettings) => (
      <ModelListSection
        defaultValues={{
          'excel_model_list.models': settings['excel_model_list.models'],
        }}
      />
    ),
  },
  {
    id: 'version-check',
    titleKey: 'Version Check',
    build: (settings: ExcelSettings) => (
      <VersionCheckSection
        defaultValues={{
          'excel_version_check.minimum_versions':
            settings['excel_version_check.minimum_versions'],
        }}
      />
    ),
  },
] as const

export type ExcelSectionId = (typeof EXCEL_SECTIONS)[number]['id']

const excelRegistry = createSectionRegistry<ExcelSectionId, ExcelSettings>({
  sections: EXCEL_SECTIONS,
  defaultSection: 'tmp-key',
  basePath: '/system-settings/excel',
  urlStyle: 'path',
})

export const EXCEL_SECTION_IDS = excelRegistry.sectionIds
export const EXCEL_DEFAULT_SECTION = excelRegistry.defaultSection
export const getExcelSectionNavItems = excelRegistry.getSectionNavItems
export const getExcelSectionContent = excelRegistry.getSectionContent
export const getExcelSectionMeta = excelRegistry.getSectionMeta
