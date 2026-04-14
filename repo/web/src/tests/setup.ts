import '@testing-library/jest-dom'
import { vi, beforeEach } from 'vitest'

// Mock fetch globally for unit tests
Object.defineProperty(globalThis, 'fetch', {
  writable: true,
  value: vi.fn(),
})

// Reset mocks between tests
beforeEach(() => {
  vi.clearAllMocks()
})
