import { createFileRoute } from '@tanstack/react-router'
import { TermsOfService } from '@/features/legal'

export const Route = createFileRoute('/terms-of-service')({
  component: TermsOfService,
})
