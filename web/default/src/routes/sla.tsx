import { createFileRoute } from '@tanstack/react-router'
import { SLA } from '@/features/legal'

export const Route = createFileRoute('/sla')({
  component: SLA,
})
