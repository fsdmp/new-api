import { createFileRoute } from '@tanstack/react-router'
import { DocsContent } from '@/features/docs'

export const Route = createFileRoute('/docs/$category/$slug')({
  component: DocsContent,
})
