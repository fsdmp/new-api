import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { ChevronRight } from 'lucide-react'
import { getCategoryById, getPageMeta } from './config/docs-nav'

interface DocsBreadcrumbProps {
  category: string
  slug: string
}

export function DocsBreadcrumb({ category, slug }: DocsBreadcrumbProps) {
  const { t } = useTranslation()
  const categoryInfo = getCategoryById(category)
  const pageInfo = getPageMeta(category, slug)

  if (!categoryInfo || !pageInfo) return null

  return (
    <nav className='text-muted-foreground mb-4 flex items-center gap-1 text-sm'>
      <Link
        to='/docs'
        className='hover:text-foreground transition-colors'
      >
        {t('Docs')}
      </Link>
      <ChevronRight className='h-3 w-3' />
      <span className='hover:text-foreground transition-colors'>
        {t(categoryInfo.titleKey)}
      </span>
      {pageInfo.slug !== 'index' && (
        <>
          <ChevronRight className='h-3 w-3' />
          <span className='text-foreground'>{t(pageInfo.titleKey)}</span>
        </>
      )}
    </nav>
  )
}
