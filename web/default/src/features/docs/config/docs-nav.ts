import {
  Rocket,
  Download,
  BookOpen,
  HelpCircle,
  FileText,
  type LucideIcon,
} from 'lucide-react'

export interface DocsNavItem {
  slug: string
  titleKey: string
}

export interface DocsNavCategory {
  id: string
  titleKey: string
  icon: LucideIcon
  items: DocsNavItem[]
}

export const docsNavCategories: DocsNavCategory[] = [
  {
    id: 'quick-start',
    titleKey: 'Quick Start',
    icon: Rocket,
    items: [
      { slug: 'introduction', titleKey: 'Introduction' },
      { slug: 'installation', titleKey: 'Installation' },
      { slug: 'configuration', titleKey: 'Configuration' },
    ],
  },
  {
    id: 'download',
    titleKey: 'Download',
    icon: Download,
    items: [
      { slug: 'aiexcel', titleKey: 'AI Excel' },
      { slug: 'browser-extension', titleKey: 'Browser Extension' },
    ],
  },
  {
    id: 'api-guide',
    titleKey: 'API Guide',
    icon: BookOpen,
    items: [
      { slug: 'authentication', titleKey: 'Authentication' },
      { slug: 'chat-completions', titleKey: 'Chat Completions' },
      { slug: 'models', titleKey: 'Models' },
      { slug: 'rate-limits', titleKey: 'Rate Limits' },
    ],
  },
  {
    id: 'faq',
    titleKey: 'FAQ',
    icon: HelpCircle,
    items: [
      { slug: 'general', titleKey: 'General' },
      { slug: 'billing', titleKey: 'Billing' },
      { slug: 'troubleshooting', titleKey: 'Troubleshooting' },
    ],
  },
  {
    id: 'changelog',
    titleKey: 'Changelog',
    icon: FileText,
    items: [
      { slug: 'index', titleKey: 'Changelog' },
    ],
  },
]

export function getFirstPageForCategory(categoryId: string): { category: string; slug: string } | null {
  const category = docsNavCategories.find((c) => c.id === categoryId)
  if (!category || category.items.length === 0) return null
  return { category: categoryId, slug: category.items[0].slug }
}

export function getCategoryById(categoryId: string): DocsNavCategory | undefined {
  return docsNavCategories.find((c) => c.id === categoryId)
}

export function getPageMeta(categoryId: string, slug: string): DocsNavItem | undefined {
  const category = getCategoryById(categoryId)
  return category?.items.find((item) => item.slug === slug)
}
