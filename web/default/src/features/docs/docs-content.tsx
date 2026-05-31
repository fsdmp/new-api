import { useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Markdown } from '@/components/ui/markdown'
import { getDocContent } from './content-registry'
import { DocsBreadcrumb } from './docs-breadcrumb'
import { FileWarning } from 'lucide-react'

export function DocsContent() {
  const { category, slug } = useParams({ strict: false }) as {
    category: string
    slug: string
  }
  const { i18n, t } = useTranslation()
  const content = getDocContent(i18n.language, category, slug)

  if (!content) {
    return (
      <div className='flex flex-1 flex-col items-center justify-center py-20'>
        <FileWarning className='text-muted-foreground mb-4 h-12 w-12' />
        <h2 className='text-xl font-semibold'>{t('Page not found')}</h2>
        <p className='text-muted-foreground mt-2'>
          {t('The requested documentation page could not be found.')}
        </p>
      </div>
    )
  }

  return (
    <div className='mx-auto max-w-4xl px-4 py-8 md:px-8'>
      <DocsBreadcrumb category={category} slug={slug} />
      <Markdown>{content}</Markdown>
    </div>
  )
}
