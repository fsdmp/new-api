import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/docs/')({
  beforeLoad: () => {
    throw redirect({
      to: '/docs/$category/$slug',
      params: { category: 'quick-start', slug: 'introduction' },
    })
  },
})
