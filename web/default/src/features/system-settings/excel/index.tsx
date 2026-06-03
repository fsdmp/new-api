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
import { SettingsPage } from '../components/settings-page'
import type { ExcelSettings } from '../types'
import {
  EXCEL_DEFAULT_SECTION,
  getExcelSectionContent,
  getExcelSectionMeta,
} from './section-registry.tsx'

const defaultExcelSettings: ExcelSettings = {
  'excel_tmp_key.enabled': false,
  'excel_tmp_key.account': '',
  'excel_tmp_key.expire_days': 7,
  'excel_tmp_key.quota': 500000,
  'excel_version_check.minimum_versions': '{}',
}

export function ExcelSettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/excel/$section'
      defaultSettings={defaultExcelSettings}
      defaultSection={EXCEL_DEFAULT_SECTION}
      getSectionContent={getExcelSectionContent}
      getSectionMeta={getExcelSectionMeta}
    />
  )
}
