import { useState, useMemo } from 'react'
import { Link, useMatchRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from '@/components/ui/collapsible'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Input } from '@/components/ui/input'
import { Search, ChevronDown } from 'lucide-react'
import { docsNavCategories, type DocsNavCategory } from './config/docs-nav'

interface DocsSidebarProps {
  onNavClick?: () => void
}

export function DocsSidebar({ onNavClick }: DocsSidebarProps) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const matchRoute = useMatchRoute()

  const filteredCategories = useMemo(() => {
    if (!search.trim()) return docsNavCategories

    const query = search.toLowerCase()
    return docsNavCategories
      .map((category) => {
        const filteredItems = category.items.filter(
          (item) =>
            t(item.titleKey).toLowerCase().includes(query) ||
            t(category.titleKey).toLowerCase().includes(query)
        )
        if (filteredItems.length === 0) return null
        return { ...category, items: filteredItems }
      })
      .filter(Boolean) as DocsNavCategory[]
  }, [search, t])

  return (
    <div className='flex h-full flex-col'>
      <div className='p-4'>
        <div className='relative'>
          <Search className='text-muted-foreground absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2' />
          <Input
            type='text'
            placeholder={t('Search docs...')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className='pl-9'
          />
        </div>
      </div>
      <ScrollArea className='flex-1 px-2'>
        <nav className='space-y-1 pb-4'>
          {filteredCategories.map((category) => {
            const isCategoryActive = matchRoute({
              to: '/docs/$category/$slug',
              params: { category: category.id },
              fuzzy: true,
            })

            return (
              <Collapsible key={category.id} defaultOpen={!!isCategoryActive}>
                <CollapsibleTrigger className='hover:bg-accent flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors'>
                  <category.icon className='h-4 w-4' />
                  <span className='flex-1 text-left'>{t(category.titleKey)}</span>
                  <ChevronDown className='text-muted-foreground h-4 w-4 transition-transform [[data-panel-open]>&]:rotate-180' />
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <div className='ml-4 space-y-0.5 border-l pl-3'>
                    {category.items.map((item) => {
                      const isActive = matchRoute({
                        to: '/docs/$category/$slug',
                        params: { category: category.id, slug: item.slug },
                      })

                      return (
                        <Link
                          key={item.slug}
                          to='/docs/$category/$slug'
                          params={{ category: category.id, slug: item.slug }}
                          onClick={onNavClick}
                          className={`block rounded-md px-3 py-1.5 text-sm transition-colors ${
                            isActive
                              ? 'bg-accent text-accent-foreground font-medium'
                              : 'text-muted-foreground hover:bg-accent hover:text-foreground'
                          }`}
                        >
                          {t(item.titleKey)}
                        </Link>
                      )
                    })}
                  </div>
                </CollapsibleContent>
              </Collapsible>
            )
          })}
        </nav>
      </ScrollArea>
    </div>
  )
}
