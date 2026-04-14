// LoginPage — local username/password login with optional MFA step.
import { useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useAuthStore } from '../../app/store'
import { api, PortalApiError } from '../../app/api/client'
import { BookOpen, Lock, ArrowRight, ShieldCheck } from 'lucide-react'

const loginInputClass = 'login-input w-full rounded-xl px-4 py-3 text-sm text-white placeholder-zinc-600 focus:outline-none transition-all'
const loginInputStyle = {
  background: 'rgba(255,255,255,0.04)',
  border: '1px solid rgba(255,255,255,0.08)',
}

const loginSchema = z.object({
  username: z.string().min(1, 'Username is required'),
  password: z.string().min(1, 'Password is required'),
})

const mfaSchema = z.object({
  code: z.string().length(6, 'Enter the 6-digit code from your authenticator app'),
})

type LoginForm = z.infer<typeof loginSchema>
type MfaForm   = z.infer<typeof mfaSchema>

interface LoginResponse {
  requires_mfa: boolean
  session_id?: string
  user?: {
    id: string
    username: string
    display_name: string
    roles: string[]
    permissions: string[]
    force_password_reset: boolean
    mfa_enrolled: boolean
    mfa_verified: boolean
  }
  compatibility_mode: 'full' | 'read_only' | 'blocked' | 'warn'
}

// SVG pattern for the background — abstract grid of knowledge/learning motifs
function BackgroundPattern() {
  return (
    <svg className="absolute inset-0 w-full h-full" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <pattern id="grid" width="60" height="60" patternUnits="userSpaceOnUse">
          <path d="M 60 0 L 0 0 0 60" fill="none" stroke="rgba(245,158,11,0.04)" strokeWidth="1" />
        </pattern>
        <radialGradient id="glow1" cx="30%" cy="20%" r="50%">
          <stop offset="0%" stopColor="rgba(245,158,11,0.08)" />
          <stop offset="100%" stopColor="rgba(245,158,11,0)" />
        </radialGradient>
        <radialGradient id="glow2" cx="70%" cy="80%" r="40%">
          <stop offset="0%" stopColor="rgba(99,102,241,0.06)" />
          <stop offset="100%" stopColor="rgba(99,102,241,0)" />
        </radialGradient>
      </defs>
      <rect width="100%" height="100%" fill="url(#grid)" />
      <rect width="100%" height="100%" fill="url(#glow1)" />
      <rect width="100%" height="100%" fill="url(#glow2)" />
      {/* Floating decorative circles */}
      <circle cx="15%" cy="25%" r="120" fill="rgba(245,158,11,0.03)" />
      <circle cx="80%" cy="15%" r="80" fill="rgba(99,102,241,0.03)" />
      <circle cx="60%" cy="75%" r="160" fill="rgba(245,158,11,0.02)" />
      <circle cx="25%" cy="85%" r="60" fill="rgba(139,92,246,0.03)" />
      {/* Abstract connection lines */}
      <line x1="10%" y1="30%" x2="40%" y2="10%" stroke="rgba(245,158,11,0.04)" strokeWidth="1" />
      <line x1="60%" y1="20%" x2="90%" y2="40%" stroke="rgba(99,102,241,0.03)" strokeWidth="1" />
      <line x1="20%" y1="70%" x2="50%" y2="90%" stroke="rgba(245,158,11,0.03)" strokeWidth="1" />
    </svg>
  )
}

export function LoginPage() {
  const [step, setStep]     = useState<'credentials' | 'mfa'>('credentials')
  const [error, setError]   = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const navigate             = useNavigate()
  const location             = useLocation()
  const { setUser, setCompatibilityMode } = useAuthStore()
  const from = (location.state as { from?: { pathname: string } })?.from?.pathname ?? '/'

  const loginForm = useForm<LoginForm>({ resolver: zodResolver(loginSchema) })
  const mfaForm   = useForm<MfaForm>({ resolver: zodResolver(mfaSchema) })

  async function onCredentialsSubmit(data: LoginForm) {
    setLoading(true)
    setError(null)
    try {
      const res = await api.post<LoginResponse>('/auth/login', data)
      if (res.requires_mfa) {
        setStep('mfa')
      } else {
        finishLogin(res)
      }
    } catch (err) {
      if (err instanceof PortalApiError) {
        setError(err.error.message)
      } else {
        setError('An unexpected error occurred.')
      }
    } finally {
      setLoading(false)
    }
  }

  async function onMfaSubmit(data: MfaForm) {
    setLoading(true)
    setError(null)
    try {
      await api.post<LoginResponse>('/auth/mfa/verify', { code: data.code })
      const session = await api.get<LoginResponse>('/session')
      finishLogin(session)
    } catch (err) {
      if (err instanceof PortalApiError) {
        setError(err.error.message)
      } else {
        setError('An unexpected error occurred.')
      }
    } finally {
      setLoading(false)
    }
  }

  function finishLogin(res: LoginResponse) {
    if (res.compatibility_mode === 'blocked') {
      setCompatibilityMode('blocked')
      navigate('/version-blocked', { replace: true })
      return
    }
    if (res.user) {
      setUser({
        id:                 res.user.id,
        username:           res.user.username,
        displayName:        res.user.display_name,
        roles:              res.user.roles,
        permissions:        res.user.permissions,
        forcePasswordReset: res.user.force_password_reset,
        mfaEnrolled:        res.user.mfa_enrolled,
        mfaVerified:        res.user.mfa_verified,
      })
      setCompatibilityMode(res.compatibility_mode)
    }
    if (res.user?.force_password_reset) {
      navigate('/account/security?reason=bootstrap_rotation', { replace: true })
    } else {
      navigate(from, { replace: true })
    }
  }

  return (
    <div
      className="relative flex min-h-screen items-center justify-center p-4 overflow-hidden"
      style={{ background: 'linear-gradient(160deg, #0a0d14 0%, #111827 40%, #0d1117 80%, #0a0d14 100%)' }}
    >
      <style>{`
        .login-input:focus {
          border-color: rgba(245,158,11,0.4) !important;
          box-shadow: 0 0 0 3px rgba(245,158,11,0.1);
        }
      `}</style>
      <BackgroundPattern />

      {/* Floating decorative elements */}
      <div className="absolute top-10 left-10 opacity-20">
        <BookOpen size={48} className="text-amber-500/30" />
      </div>
      <div className="absolute bottom-20 right-16 opacity-15">
        <ShieldCheck size={36} className="text-indigo-400/20" />
      </div>

      {/* Login card */}
      <div
        className="relative z-10 w-full max-w-[400px] rounded-2xl p-8 backdrop-blur-sm"
        style={{
          background: 'linear-gradient(145deg, rgba(17,24,39,0.9) 0%, rgba(15,18,25,0.95) 100%)',
          border: '1px solid rgba(255,255,255,0.08)',
          boxShadow: '0 24px 64px rgba(0,0,0,0.5), 0 0 0 1px rgba(255,255,255,0.03), inset 0 1px 0 rgba(255,255,255,0.04)',
        }}
      >
        {/* Brand header */}
        <div className="flex items-center gap-3 mb-6">
          <div
            className="h-11 w-11 rounded-xl flex items-center justify-center"
            style={{
              background: 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)',
              boxShadow: '0 4px 16px rgba(245,158,11,0.3)',
            }}
          >
            <BookOpen size={20} className="text-white" />
          </div>
          <div>
            <h1 className="text-lg font-bold text-white tracking-tight">Welcome back</h1>
            <p className="text-xs text-zinc-500">Workforce Learning &amp; Procurement Portal</p>
          </div>
        </div>

        {error && (
          <div
            className="mb-5 rounded-xl px-4 py-3 text-sm"
            style={{
              background: 'rgba(239,68,68,0.1)',
              border: '1px solid rgba(239,68,68,0.2)',
              color: '#f87171',
            }}
            role="alert"
          >
            {error}
          </div>
        )}

        {step === 'credentials' && (
          <form onSubmit={loginForm.handleSubmit(onCredentialsSubmit)} noValidate>
            <div className="mb-4">
              <label htmlFor="username" className="mb-1.5 block text-xs font-medium text-zinc-400 uppercase tracking-wider">
                Username
              </label>
              <input
                id="username"
                type="text"
                autoComplete="username"
                className={loginInputClass}
                style={loginInputStyle}
                placeholder="Enter your username"
                {...loginForm.register('username')}
              />
              {loginForm.formState.errors.username && (
                <p className="mt-1.5 text-xs text-red-400">{loginForm.formState.errors.username.message}</p>
              )}
            </div>

            <div className="mb-6">
              <label htmlFor="password" className="mb-1.5 block text-xs font-medium text-zinc-400 uppercase tracking-wider">
                Password
              </label>
              <div className="relative">
                <Lock size={14} className="absolute left-4 top-1/2 -translate-y-1/2 text-zinc-600" />
                <input
                  id="password"
                  type="password"
                  autoComplete="current-password"
                  className="login-input w-full rounded-xl pl-10 pr-4 py-3 text-sm text-white placeholder-zinc-600 focus:outline-none transition-all"
                  style={loginInputStyle}
                  placeholder="Enter your password"
                  {...loginForm.register('password')}
                />
              </div>
              {loginForm.formState.errors.password && (
                <p className="mt-1.5 text-xs text-red-400">{loginForm.formState.errors.password.message}</p>
              )}
            </div>

            <button
              type="submit"
              disabled={loading}
              className="w-full flex items-center justify-center gap-2 rounded-xl px-4 py-3 text-sm font-semibold text-white transition-all disabled:opacity-50"
              style={{
                background: 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)',
                boxShadow: loading ? 'none' : '0 4px 16px rgba(245,158,11,0.3), inset 0 1px 0 rgba(255,255,255,0.15)',
              }}
            >
              {loading ? 'Signing in...' : (
                <>Sign in <ArrowRight size={14} /></>
              )}
            </button>
          </form>
        )}

        {step === 'mfa' && (
          <form onSubmit={mfaForm.handleSubmit(onMfaSubmit)} noValidate>
            <div className="flex items-center gap-2 mb-4">
              <ShieldCheck size={16} className="text-amber-400" />
              <p className="text-sm text-zinc-400">
                Enter the 6-digit code from your authenticator app.
              </p>
            </div>
            <div className="mb-6">
              <input
                id="code"
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
                className="login-input w-full rounded-xl px-4 py-4 text-center text-xl tracking-[0.3em] font-mono text-white placeholder-zinc-600 focus:outline-none transition-all"
                style={loginInputStyle}
                placeholder="000000"
                {...mfaForm.register('code')}
              />
              {mfaForm.formState.errors.code && (
                <p className="mt-1.5 text-xs text-red-400">{mfaForm.formState.errors.code.message}</p>
              )}
            </div>

            <button
              type="submit"
              disabled={loading}
              className="w-full flex items-center justify-center gap-2 rounded-xl px-4 py-3 text-sm font-semibold text-white transition-all disabled:opacity-50"
              style={{
                background: 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)',
                boxShadow: loading ? 'none' : '0 4px 16px rgba(245,158,11,0.3), inset 0 1px 0 rgba(255,255,255,0.15)',
              }}
            >
              {loading ? 'Verifying...' : (
                <>Verify <ShieldCheck size={14} /></>
              )}
            </button>
            <button
              type="button"
              className="mt-3 w-full text-xs text-zinc-500 hover:text-zinc-300 transition-colors"
              onClick={() => setStep('credentials')}
            >
              Back to sign in
            </button>
          </form>
        )}

        {/* Footer */}
        <div className="mt-6 pt-5 text-center" style={{ borderTop: '1px solid rgba(255,255,255,0.05)' }}>
          <p className="text-[10px] text-zinc-600">
            Offline deployment &middot; All data stays on-premises
          </p>
        </div>
      </div>
    </div>
  )
}
