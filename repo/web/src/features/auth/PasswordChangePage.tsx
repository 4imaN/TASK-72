// PasswordChangePage — forced password rotation for bootstrap accounts,
// and voluntary password changes for authenticated users.
import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { ShieldCheck, Eye, EyeOff, AlertTriangle, CheckCircle2 } from 'lucide-react'
import { useAuthStore } from '../../app/store'
import { api, PortalApiError } from '../../app/api/client'

// ── Schema ────────────────────────────────────────────────────────────────────

const changeSchema = z
  .object({
    currentPassword: z.string().optional(),
    newPassword: z
      .string()
      .min(8, 'Password must be at least 8 characters')
      .max(128, 'Password must be 128 characters or fewer'),
    confirmPassword: z.string().min(1, 'Please confirm your new password'),
  })
  .refine((d) => d.newPassword === d.confirmPassword, {
    message: 'Passwords do not match',
    path: ['confirmPassword'],
  })

type ChangeForm = z.infer<typeof changeSchema>

// ── Strength meter ────────────────────────────────────────────────────────────

function measureStrength(password: string): { score: number; label: string; color: string } {
  if (!password) return { score: 0, label: '', color: 'bg-border' }
  let score = 0
  if (password.length >= 8)  score++
  if (password.length >= 12) score++
  if (/[A-Z]/.test(password)) score++
  if (/[0-9]/.test(password)) score++
  if (/[^A-Za-z0-9]/.test(password)) score++

  if (score <= 1) return { score, label: 'Weak',   color: 'bg-destructive' }
  if (score <= 3) return { score, label: 'Fair',   color: 'bg-amber-500'   }
  if (score <= 4) return { score, label: 'Strong', color: 'bg-emerald-500' }
  return              { score, label: 'Very strong', color: 'bg-emerald-600' }
}

function StrengthMeter({ password }: { password: string }) {
  const { score, label, color } = measureStrength(password)
  if (!password) return null

  return (
    <div className="mt-2 space-y-1">
      <div className="flex gap-1">
        {[1, 2, 3, 4, 5].map((n) => (
          <div
            key={n}
            className={`h-1 flex-1 rounded-full transition-all duration-300 ${
              n <= score ? color : 'bg-border'
            }`}
          />
        ))}
      </div>
      {label && (
        <p className={`text-xs font-medium ${
          score <= 1 ? 'text-destructive' :
          score <= 3 ? 'text-amber-600' :
          'text-emerald-600'
        }`}>
          {label}
        </p>
      )}
    </div>
  )
}

// ── Password input with reveal toggle ────────────────────────────────────────

interface PasswordInputProps {
  id: string
  placeholder?: string
  autoComplete?: string
  error?: string
  registration: ReturnType<ReturnType<typeof useForm<ChangeForm>>['register']>
}

function PasswordInput({ id, placeholder, autoComplete, error, registration }: PasswordInputProps) {
  const [visible, setVisible] = useState(false)

  return (
    <div className="relative">
      <input
        id={id}
        type={visible ? 'text' : 'password'}
        autoComplete={autoComplete}
        placeholder={placeholder}
        className={`w-full rounded-md border bg-background px-3 py-2 pr-10 text-sm placeholder:text-muted-foreground/60 focus:outline-none focus:ring-2 focus:ring-ring transition-shadow ${
          error ? 'border-destructive focus:ring-destructive/30' : ''
        }`}
        {...registration}
      />
      <button
        type="button"
        tabIndex={-1}
        onClick={() => setVisible((v) => !v)}
        className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
        aria-label={visible ? 'Hide password' : 'Show password'}
      >
        {visible ? <EyeOff size={15} /> : <Eye size={15} />}
      </button>
    </div>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

export function PasswordChangePage() {
  const [searchParams] = useSearchParams()
  const navigate        = useNavigate()
  const { user, setUser } = useAuthStore()

  const isBootstrap = searchParams.get('reason') === 'bootstrap_rotation'
  const [error,    setError]    = useState<string | null>(null)
  const [success,  setSuccess]  = useState(false)
  const [loading,  setLoading]  = useState(false)

  const form = useForm<ChangeForm>({
    resolver: zodResolver(changeSchema),
    defaultValues: { currentPassword: '', newPassword: '', confirmPassword: '' },
  })

  const watchedNew = form.watch('newPassword') ?? ''

  async function onSubmit(data: ChangeForm) {
    setLoading(true)
    setError(null)
    try {
      await api.post('/auth/password/change', {
        current_password: data.currentPassword ?? undefined,
        new_password:     data.newPassword,
      })

      setSuccess(true)

      // Clear the force_password_reset flag in client state.
      if (user) {
        setUser({ ...user, forcePasswordReset: false })
      }

      // Navigate after a brief success pause so the user sees confirmation.
      setTimeout(() => navigate('/', { replace: true }), 1800)
    } catch (err) {
      if (err instanceof PortalApiError) {
        setError(err.error.message)
      } else {
        setError('An unexpected error occurred. Please try again.')
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background text-foreground p-4">
      {/* Subtle geometric background pattern */}
      <div
        className="pointer-events-none fixed inset-0 opacity-[0.025]"
        aria-hidden
        style={{
          backgroundImage:
            'repeating-linear-gradient(45deg, currentColor 0, currentColor 1px, transparent 0, transparent 50%)',
          backgroundSize: '20px 20px',
        }}
      />

      <div className="relative w-full max-w-md">
        {/* Bootstrap banner */}
        {isBootstrap && (
          <div className="mb-4 flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
            <AlertTriangle size={16} className="mt-0.5 flex-shrink-0" />
            <div>
              <p className="font-semibold">Password rotation required</p>
              <p className="mt-0.5 text-amber-700">
                Your account was provisioned with a temporary password. You must
                set a personal password before continuing.
              </p>
            </div>
          </div>
        )}

        <div className="rounded-xl border bg-card shadow-sm">
          {/* Header stripe */}
          <div className="flex items-center gap-3 border-b px-6 py-5">
            <div className="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg bg-primary/10">
              <ShieldCheck size={18} className="text-primary" />
            </div>
            <div>
              <h1 className="text-base font-semibold leading-tight">
                {isBootstrap ? 'Set your password' : 'Change password'}
              </h1>
              <p className="text-xs text-muted-foreground">
                Workforce Learning &amp; Procurement Portal
              </p>
            </div>
          </div>

          {/* Body */}
          <div className="px-6 py-6">
            {success ? (
              <div className="flex flex-col items-center gap-3 py-6 text-center">
                <CheckCircle2 size={40} className="text-emerald-500" />
                <p className="font-semibold text-foreground">Password updated</p>
                <p className="text-sm text-muted-foreground">
                  Redirecting you to the portal&hellip;
                </p>
              </div>
            ) : (
              <form onSubmit={form.handleSubmit(onSubmit)} noValidate className="space-y-4">
                {/* Error */}
                {error && (
                  <div
                    className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive"
                    role="alert"
                  >
                    <AlertTriangle size={14} className="mt-0.5 flex-shrink-0" />
                    <span>{error}</span>
                  </div>
                )}

                {/* Current password — only shown for regular (non-bootstrap) change */}
                {!isBootstrap && (
                  <div>
                    <label htmlFor="currentPassword" className="mb-1.5 block text-sm font-medium">
                      Current password
                    </label>
                    <PasswordInput
                      id="currentPassword"
                      autoComplete="current-password"
                      placeholder="Your current password"
                      error={form.formState.errors.currentPassword?.message}
                      registration={form.register('currentPassword')}
                    />
                    {form.formState.errors.currentPassword && (
                      <p className="mt-1 text-xs text-destructive">
                        {form.formState.errors.currentPassword.message}
                      </p>
                    )}
                  </div>
                )}

                {/* New password */}
                <div>
                  <label htmlFor="newPassword" className="mb-1.5 block text-sm font-medium">
                    New password
                  </label>
                  <PasswordInput
                    id="newPassword"
                    autoComplete="new-password"
                    placeholder="At least 8 characters"
                    error={form.formState.errors.newPassword?.message}
                    registration={form.register('newPassword')}
                  />
                  {form.formState.errors.newPassword ? (
                    <p className="mt-1 text-xs text-destructive">
                      {form.formState.errors.newPassword.message}
                    </p>
                  ) : (
                    <StrengthMeter password={watchedNew} />
                  )}
                </div>

                {/* Confirm password */}
                <div>
                  <label htmlFor="confirmPassword" className="mb-1.5 block text-sm font-medium">
                    Confirm new password
                  </label>
                  <PasswordInput
                    id="confirmPassword"
                    autoComplete="new-password"
                    placeholder="Repeat the new password"
                    error={form.formState.errors.confirmPassword?.message}
                    registration={form.register('confirmPassword')}
                  />
                  {form.formState.errors.confirmPassword && (
                    <p className="mt-1 text-xs text-destructive">
                      {form.formState.errors.confirmPassword.message}
                    </p>
                  )}
                </div>

                {/* Requirements checklist */}
                <div className="rounded-md bg-muted px-3 py-2.5 text-xs text-muted-foreground space-y-1">
                  <p className="font-medium text-foreground/70 mb-1">Requirements</p>
                  {[
                    { met: watchedNew.length >= 8,            label: 'At least 8 characters' },
                    { met: /[A-Z]/.test(watchedNew),          label: 'One uppercase letter'  },
                    { met: /[0-9]/.test(watchedNew),          label: 'One number'            },
                    { met: /[^A-Za-z0-9]/.test(watchedNew),   label: 'One special character' },
                  ].map(({ met, label }) => (
                    <div key={label} className="flex items-center gap-1.5">
                      <span className={`text-[10px] ${met ? 'text-emerald-600' : 'text-border'}`}>
                        {met ? '✓' : '○'}
                      </span>
                      <span className={met ? 'text-foreground/80' : ''}>{label}</span>
                    </div>
                  ))}
                </div>

                {/* Submit */}
                <button
                  type="submit"
                  disabled={loading}
                  className="w-full rounded-md bg-primary px-4 py-2.5 text-sm font-medium text-primary-foreground hover:bg-primary/90 active:scale-[0.99] disabled:cursor-not-allowed disabled:opacity-50 transition-all"
                >
                  {loading ? (
                    <span className="flex items-center justify-center gap-2">
                      <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-primary-foreground border-t-transparent" />
                      Updating…
                    </span>
                  ) : (
                    isBootstrap ? 'Set password & continue' : 'Update password'
                  )}
                </button>

                {/* Escape hatch — only for non-bootstrap users */}
                {!isBootstrap && (
                  <button
                    type="button"
                    onClick={() => navigate(-1)}
                    className="w-full text-center text-xs text-muted-foreground underline-offset-2 hover:underline"
                  >
                    Cancel
                  </button>
                )}
              </form>
            )}
          </div>
        </div>

        {/* Footer note */}
        <p className="mt-3 text-center text-xs text-muted-foreground">
          Your password is encrypted and never stored in plain text.
        </p>
      </div>
    </div>
  )
}
