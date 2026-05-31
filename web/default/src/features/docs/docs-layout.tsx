import { useState } from 'react'
import { Outlet } from '@tanstack/react-router'
import { PublicLayout } from '@/components/layout'
import { DocsSidebar } from './docs-sidebar'
import { Sheet, SheetTrigger, SheetContent } from '@/components/ui/sheet'
import { Button } from '@/components/ui/button'
import { Menu } from 'lucide-react'
import { useTranslation } from 'react-i18next'

export function DocsLayout() {
  const [mobileOpen, setMobileOpen] = useState(false)
  const { t } = useTranslation()

  return (
    <PublicLayout showMainContainer={false}>
      <div className='pt-14'>
        {/* Mobile sidebar toggle */}
        <div className='sticky top-14 z-30 flex items-center border-b bg-background px-4 py-2 md:hidden'>
          <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
            <SheetTrigger
              render={
                <Button variant='ghost' size='sm'>
                  <Menu className='mr-2 h-4 w-4' />
                  {t('Menu')}
                </Button>
              }
            />
            <SheetContent side='left' className='w-72 p-0'>
              <DocsSidebar onNavClick={() => setMobileOpen(false)} />
            </SheetContent>
          </Sheet>
        </div>

        <div className='flex'>
          {/* Desktop sidebar */}
          <aside className='hidden w-64 shrink-0 border-r md:block'>
            <div className='sticky top-14 h-[calc(100vh-3.5rem)] overflow-y-auto'>
              <DocsSidebar />
            </div>
          </aside>

          {/* Main content */}
          <main className='min-h-[calc(100vh-3.5rem)] flex-1 overflow-y-auto'>
            <Outlet />
          </main>
        </div>
      </div>
    </PublicLayout>
  )
}
