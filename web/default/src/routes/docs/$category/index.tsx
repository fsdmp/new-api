import { createFileRoute, redirect } from '@tanstack/react-router'
import { getFirstPageForCategory } from '@/features/docs'

export const Route = createFileRoute('/docs/$category/')({
  beforeLoad: ({ params }) => {
    const firstPage = getFirstPageForCategory(params.category)
    if (firstPage) {
      throw redirect({
        to: '/docs/$category/$slug',
        params: { category: firstPage.category, slug: firstPage.slug },
      })
    }
    // If category not found, redirect to docs home
    throw redirect({ to: '/docs' })
  },
})
