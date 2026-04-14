// App version — injected at build time via Vite define or falls back to package version.
export const APP_VERSION: string = (import.meta as unknown as { env: Record<string, string> }).env?.VITE_APP_VERSION ?? '0.1.0'

// Sent with every API request as X-Client-Version header so the backend
// can enforce compatibility rules and return read-only grace mode responses.
export const CLIENT_VERSION_HEADER = 'X-Client-Version'
