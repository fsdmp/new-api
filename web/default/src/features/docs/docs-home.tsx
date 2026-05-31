import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { docsNavCategories } from './config/docs-nav'
import { getFirstPageForCategory } from './config/docs-nav'
import { ArrowRight } from 'lucide-react'

export function DocsHome() {
  const { t } = useTranslation()

  return (
    <div className='mx-auto max-w-4xl px-4 py-12 md:px-8 md:py-16'>
      <div className='mb-10 text-center'>
        <h1 className='text-3xl font-bold tracking-tight md:text-4xl'>
          {t('Documentation')}
        </h1>
        <p className='text-muted-foreground mt-3 text-base'>
          {t('Everything you need to get started and make the most of the platform.')}
        </p>
      </div>

      <div className='grid gap-4 sm:grid-cols-2'>
        {docsNavCategories.map((category) => {
          const firstPage = getFirstPageForCategory(category.id)
          if (!firstPage) return null

          return (
            <Link
              key={category.id}
              to='/docs/$category/$slug'
              params={{ category: firstPage.category, slug: firstPage.slug }}
              className='hover:bg-accent/50 group rounded-xl border p-5 transition-colors'
            >
              <div className='flex items-start justify-between'>
                <div className='flex items-center gap-3'>
                  <div className='bg-primary/10 text-primary flex size-9 items-center justify-center rounded-lg'>
                    <category.icon className='size-4' />
                  </div>
                  <h2 className='font-semibold'>{t(category.titleKey)}</h2>
                </div>
                <ArrowRight className='text-muted-foreground/50 group-hover:text-foreground mt-0.5 size-4 transition-colors' />
              </div>
              <div className='mt-3 flex flex-wrap gap-1.5'>
                {category.items.map((item) => (
                  <span
                    key={item.slug}
                    className='text-muted-foreground bg-muted/50 rounded-md px-2 py-0.5 text-xs'
                  >
                    {t(item.titleKey)}
                  </span>
                ))}
              </div>
            </Link>
          )
        })}
      </div>
    </div>
  )
}
