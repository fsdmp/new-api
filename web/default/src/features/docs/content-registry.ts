// Static imports for all markdown content files
// English
import en_quick_start_introduction from '@/content/docs/en/quick-start/introduction.md?raw'
import en_quick_start_installation from '@/content/docs/en/quick-start/installation.md?raw'
import en_quick_start_configuration from '@/content/docs/en/quick-start/configuration.md?raw'
import en_download_aiexcel from '@/content/docs/en/download/aiexcel.md?raw'
import en_download_browser_extension from '@/content/docs/en/download/browser-extension.md?raw'
import en_api_guide_authentication from '@/content/docs/en/api-guide/authentication.md?raw'
import en_api_guide_chat_completions from '@/content/docs/en/api-guide/chat-completions.md?raw'
import en_api_guide_models from '@/content/docs/en/api-guide/models.md?raw'
import en_api_guide_rate_limits from '@/content/docs/en/api-guide/rate-limits.md?raw'
import en_faq_general from '@/content/docs/en/faq/general.md?raw'
import en_faq_billing from '@/content/docs/en/faq/billing.md?raw'
import en_faq_troubleshooting from '@/content/docs/en/faq/troubleshooting.md?raw'
import en_changelog_index from '@/content/docs/en/changelog/index.md?raw'

// Chinese
import zh_quick_start_introduction from '@/content/docs/zh/quick-start/introduction.md?raw'
import zh_quick_start_installation from '@/content/docs/zh/quick-start/installation.md?raw'
import zh_quick_start_configuration from '@/content/docs/zh/quick-start/configuration.md?raw'
import zh_download_aiexcel from '@/content/docs/zh/download/aiexcel.md?raw'
import zh_download_browser_extension from '@/content/docs/zh/download/browser-extension.md?raw'
import zh_api_guide_authentication from '@/content/docs/zh/api-guide/authentication.md?raw'
import zh_api_guide_chat_completions from '@/content/docs/zh/api-guide/chat-completions.md?raw'
import zh_api_guide_models from '@/content/docs/zh/api-guide/models.md?raw'
import zh_api_guide_rate_limits from '@/content/docs/zh/api-guide/rate-limits.md?raw'
import zh_faq_general from '@/content/docs/zh/faq/general.md?raw'
import zh_faq_billing from '@/content/docs/zh/faq/billing.md?raw'
import zh_faq_troubleshooting from '@/content/docs/zh/faq/troubleshooting.md?raw'
import zh_changelog_index from '@/content/docs/zh/changelog/index.md?raw'

type ContentKey = `${string}_${string}_${string}`

const contentMap: Record<string, string> = {
  // English
  'en_quick-start_introduction': en_quick_start_introduction,
  'en_quick-start_installation': en_quick_start_installation,
  'en_quick-start_configuration': en_quick_start_configuration,
  'en_download_aiexcel': en_download_aiexcel,
  'en_download_browser-extension': en_download_browser_extension,
  'en_api-guide_authentication': en_api_guide_authentication,
  'en_api-guide_chat-completions': en_api_guide_chat_completions,
  'en_api-guide_models': en_api_guide_models,
  'en_api-guide_rate-limits': en_api_guide_rate_limits,
  'en_faq_general': en_faq_general,
  'en_faq_billing': en_faq_billing,
  'en_faq_troubleshooting': en_faq_troubleshooting,
  'en_changelog_index': en_changelog_index,

  // Chinese
  'zh_quick-start_introduction': zh_quick_start_introduction,
  'zh_quick-start_installation': zh_quick_start_installation,
  'zh_quick-start_configuration': zh_quick_start_configuration,
  'zh_download_aiexcel': zh_download_aiexcel,
  'zh_download_browser-extension': zh_download_browser_extension,
  'zh_api-guide_authentication': zh_api_guide_authentication,
  'zh_api-guide_chat-completions': zh_api_guide_chat_completions,
  'zh_api-guide_models': zh_api_guide_models,
  'zh_api-guide_rate-limits': zh_api_guide_rate_limits,
  'zh_faq_general': zh_faq_general,
  'zh_faq_billing': zh_faq_billing,
  'zh_faq_troubleshooting': zh_faq_troubleshooting,
  'zh_changelog_index': zh_changelog_index,
}

export function getDocContent(lang: string, category: string, slug: string): string | null {
  const normalizedLang = lang.startsWith('zh') ? 'zh' : 'en'
  const key = `${normalizedLang}_${category}_${slug}`

  // Try requested language first
  if (contentMap[key]) {
    return contentMap[key]
  }

  // Fallback to English
  const fallbackKey = `en_${category}_${slug}`
  return contentMap[fallbackKey] ?? null
}
