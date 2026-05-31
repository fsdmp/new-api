import { createFileRoute } from '@tanstack/react-router'
import { DocsLayout } from '@/features/docs'

export const Route = createFileRoute('/docs')({
  component: DocsLayout,
})
