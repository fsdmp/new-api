import { createFileRoute } from '@tanstack/react-router'
import { DPA } from '@/features/legal'

export const Route = createFileRoute('/dpa')({
  component: DPA,
})
