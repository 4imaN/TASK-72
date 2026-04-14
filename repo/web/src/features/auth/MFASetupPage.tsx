// MFASetupPage — TOTP enrollment flow with recovery code reveal.
// Design: dark technical panel, monospace security aesthetic, amber security accents.
import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, PortalApiError } from '../../app/api/client'
import { useAuthStore } from '../../app/store'

type Step = 'start' | 'confirm' | 'recovery'

interface EnrollStartResponse {
  provisioning_uri: string
  secret: string
}

interface EnrollConfirmResponse {
  enrolled: boolean
  recovery_codes: string[]
}

// ── Utility ──────────────────────────────────────────────────────────────────

function copyToClipboard(text: string): Promise<boolean> {
  return navigator.clipboard.writeText(text).then(() => true).catch(() => false)
}

function downloadCodes(codes: string[]) {
  const content = [
    'Portal MFA Recovery Codes',
    '=========================',
    'Keep these codes secure. Each can only be used once.',
    '',
    ...codes.map((c, i) => `${String(i + 1).padStart(2, '0')}. ${c}`),
    '',
    `Generated: ${new Date().toISOString()}`,
  ].join('\n')
  const blob = new Blob([content], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'portal-mfa-recovery-codes.txt'
  a.click()
  URL.revokeObjectURL(url)
}

// ── Sub-components ────────────────────────────────────────────────────────────

function StepIndicator({ current }: { current: Step }) {
  const steps: { key: Step; label: string; num: number }[] = [
    { key: 'start',    label: 'Setup',    num: 1 },
    { key: 'confirm',  label: 'Verify',   num: 2 },
    { key: 'recovery', label: 'Backup',   num: 3 },
  ]
  const order: Record<Step, number> = { start: 0, confirm: 1, recovery: 2 }

  return (
    <div className="mfa-steps">
      {steps.map((s, i) => {
        const done    = order[current] > i
        const active  = current === s.key
        return (
          <div key={s.key} className="mfa-step-item">
            <div className={`mfa-step-circle ${done ? 'done' : active ? 'active' : 'idle'}`}>
              {done ? (
                <svg width="12" height="12" viewBox="0 0 12 12" fill="none">
                  <path d="M2 6l3 3 5-5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"/>
                </svg>
              ) : (
                <span>{s.num}</span>
              )}
            </div>
            <span className={`mfa-step-label ${active ? 'active' : ''}`}>{s.label}</span>
            {i < steps.length - 1 && <div className={`mfa-step-line ${done ? 'done' : ''}`} />}
          </div>
        )
      })}
    </div>
  )
}

function SecretDisplay({ secret, uri }: { secret: string; uri: string }) {
  const [copied, setCopied] = useState(false)

  async function handleCopy() {
    const ok = await copyToClipboard(secret)
    if (ok) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  // Format the secret in groups of 4 for readability
  const formatted = secret.match(/.{1,4}/g)?.join(' ') ?? secret

  return (
    <div className="mfa-secret-block">
      <div className="mfa-secret-label">Manual entry key</div>
      <div className="mfa-secret-value">
        <code>{formatted}</code>
        <button onClick={handleCopy} className="mfa-copy-btn" title="Copy secret">
          {copied ? (
            <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
              <path d="M2 7l3.5 3.5L12 3" stroke="#f59e0b" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"/>
            </svg>
          ) : (
            <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
              <rect x="4" y="4" width="8" height="8" rx="1" stroke="currentColor" strokeWidth="1.3"/>
              <path d="M2 10V2h8" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round"/>
            </svg>
          )}
        </button>
      </div>
      <div className="mfa-uri-block">
        <div className="mfa-secret-label">Provisioning URI</div>
        <div className="mfa-uri-text">{uri}</div>
      </div>
    </div>
  )
}

function OTPInput({ onComplete }: { onComplete: (code: string) => void }) {
  const [digits, setDigits] = useState<string[]>(Array(6).fill(''))
  const refs = useRef<(HTMLInputElement | null)[]>([])

  function handleChange(index: number, value: string) {
    const cleaned = value.replace(/\D/g, '').slice(-1)
    const next = [...digits]
    next[index] = cleaned
    setDigits(next)

    if (cleaned && index < 5) {
      refs.current[index + 1]?.focus()
    }

    const full = next.join('')
    if (full.length === 6) {
      onComplete(full)
    }
  }

  function handleKeyDown(index: number, e: React.KeyboardEvent) {
    if (e.key === 'Backspace' && !digits[index] && index > 0) {
      refs.current[index - 1]?.focus()
    }
  }

  function handlePaste(e: React.ClipboardEvent) {
    e.preventDefault()
    const pasted = e.clipboardData.getData('text').replace(/\D/g, '').slice(0, 6)
    if (!pasted) return
    const next = [...Array(6).fill('')]
    for (let i = 0; i < pasted.length; i++) next[i] = pasted[i]
    setDigits(next)
    const focusIdx = Math.min(pasted.length, 5)
    refs.current[focusIdx]?.focus()
    if (pasted.length === 6) onComplete(pasted)
  }

  return (
    <div className="otp-grid" onPaste={handlePaste}>
      {digits.map((d, i) => (
        <input
          key={i}
          ref={el => { refs.current[i] = el }}
          type="text"
          inputMode="numeric"
          maxLength={1}
          value={d}
          className={`otp-cell ${d ? 'filled' : ''}`}
          onChange={e => handleChange(i, e.target.value)}
          onKeyDown={e => handleKeyDown(i, e)}
          autoComplete={i === 0 ? 'one-time-code' : 'off'}
          autoFocus={i === 0}
        />
      ))}
    </div>
  )
}

function RecoveryCodeGrid({ codes }: { codes: string[] }) {
  const [copiedAll, setCopiedAll] = useState(false)

  async function handleCopyAll() {
    const text = codes.join('\n')
    const ok = await copyToClipboard(text)
    if (ok) {
      setCopiedAll(true)
      setTimeout(() => setCopiedAll(false), 2000)
    }
  }

  return (
    <div className="recovery-block">
      <div className="recovery-grid">
        {codes.map((code, i) => (
          <div key={i} className="recovery-code-item">
            <span className="recovery-code-num">{String(i + 1).padStart(2, '0')}</span>
            <code className="recovery-code-val">{code}</code>
          </div>
        ))}
      </div>
      <div className="recovery-actions">
        <button onClick={handleCopyAll} className="mfa-btn-secondary">
          {copiedAll ? 'Copied!' : 'Copy all codes'}
        </button>
        <button onClick={() => downloadCodes(codes)} className="mfa-btn-secondary">
          Download as .txt
        </button>
      </div>
    </div>
  )
}

// ── Main Page ─────────────────────────────────────────────────────────────────

export function MFASetupPage() {
  const [step, setStep]             = useState<Step>('start')
  const [loading, setLoading]       = useState(false)
  const [error, setError]           = useState<string | null>(null)
  const [enrollData, setEnrollData] = useState<EnrollStartResponse | null>(null)
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])

  const navigate = useNavigate()
  const { setUser, user } = useAuthStore()

  useEffect(() => {
    // Auto-start enrollment when the page mounts
    handleStart()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function handleStart() {
    setLoading(true)
    setError(null)
    try {
      const res = await api.post<EnrollStartResponse>('/mfa/enroll/start')
      setEnrollData(res)
      setStep('confirm')
    } catch (err) {
      if (err instanceof PortalApiError) {
        setError(err.error.message)
      } else {
        setError('Failed to start MFA setup. Please try again.')
      }
    } finally {
      setLoading(false)
    }
  }

  async function handleConfirm(code: string) {
    if (loading) return
    setLoading(true)
    setError(null)
    try {
      const res = await api.post<EnrollConfirmResponse>('/mfa/enroll/confirm', { code })
      setRecoveryCodes(res.recovery_codes)
      setStep('recovery')
      // Update auth store to reflect enrollment
      if (user) {
        setUser({ ...user, mfaEnrolled: true })
      }
    } catch (err) {
      if (err instanceof PortalApiError) {
        setError(err.error.message)
      } else {
        setError('Verification failed. Please try again.')
      }
    } finally {
      setLoading(false)
    }
  }

  function handleFinish() {
    navigate('/account/security', { replace: true })
  }

  return (
    <>
      <style>{MFA_STYLES}</style>
      <div className="mfa-shell">
        {/* Background grid texture */}
        <div className="mfa-bg-grid" aria-hidden="true" />

        <div className="mfa-card">
          {/* Header */}
          <div className="mfa-header">
            <div className="mfa-shield-icon" aria-hidden="true">
              <svg width="28" height="32" viewBox="0 0 28 32" fill="none">
                <path
                  d="M14 2L3 7v9c0 6.627 4.925 12.828 11 14 6.075-1.172 11-7.373 11-14V7L14 2z"
                  stroke="#f59e0b"
                  strokeWidth="1.8"
                  fill="rgba(245,158,11,0.08)"
                />
                <path
                  d="M9 16l3.5 3.5L19 12"
                  stroke="#f59e0b"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            </div>
            <div>
              <h1 className="mfa-title">Two-Factor Authentication</h1>
              <p className="mfa-subtitle">Secure your account with an authenticator app</p>
            </div>
          </div>

          {/* Step indicator */}
          <StepIndicator current={step} />

          {/* Error banner */}
          {error && (
            <div className="mfa-error" role="alert">
              <svg width="14" height="14" viewBox="0 0 14 14" fill="none" style={{ flexShrink: 0 }}>
                <circle cx="7" cy="7" r="6" stroke="#ef4444" strokeWidth="1.3"/>
                <path d="M7 4v4" stroke="#ef4444" strokeWidth="1.5" strokeLinecap="round"/>
                <circle cx="7" cy="10.5" r="0.75" fill="#ef4444"/>
              </svg>
              {error}
            </div>
          )}

          {/* ── Step: start (loading) ── */}
          {step === 'start' && (
            <div className="mfa-loading-state">
              <div className="mfa-spinner" aria-label="Loading" />
              <p>Generating your authenticator key…</p>
            </div>
          )}

          {/* ── Step: confirm ── */}
          {step === 'confirm' && enrollData && (
            <div className="mfa-step-content">
              <div className="mfa-instruction">
                <span className="mfa-instruction-num">1</span>
                <div>
                  <strong>Open your authenticator app</strong>
                  <p>Use Google Authenticator, Authy, 1Password, or any TOTP-compatible app.</p>
                </div>
              </div>
              <div className="mfa-instruction">
                <span className="mfa-instruction-num">2</span>
                <div>
                  <strong>Add a new account manually</strong>
                  <p>Enter the key below, or paste the provisioning URI into your app.</p>
                </div>
              </div>

              <SecretDisplay secret={enrollData.secret} uri={enrollData.provisioning_uri} />

              <div className="mfa-instruction" style={{ marginTop: '1.5rem' }}>
                <span className="mfa-instruction-num">3</span>
                <div>
                  <strong>Enter the 6-digit code shown in your app</strong>
                  <p>The code refreshes every 30 seconds.</p>
                </div>
              </div>

              <OTPInput onComplete={handleConfirm} />

              {loading && (
                <div className="mfa-verifying">
                  <div className="mfa-spinner-sm" />
                  Verifying…
                </div>
              )}
            </div>
          )}

          {/* ── Step: recovery ── */}
          {step === 'recovery' && (
            <div className="mfa-step-content">
              <div className="mfa-recovery-notice">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none" style={{ flexShrink: 0, marginTop: '1px' }}>
                  <path d="M8 2a6 6 0 100 12A6 6 0 008 2zm0 5v3m0-4.5h.01" stroke="#f59e0b" strokeWidth="1.4" strokeLinecap="round"/>
                </svg>
                <div>
                  <strong>Save these recovery codes now.</strong>
                  <p>
                    They are shown only once. If you lose access to your authenticator, use one of these
                    to regain entry. Each code can only be used once.
                  </p>
                </div>
              </div>

              <RecoveryCodeGrid codes={recoveryCodes} />

              <div className="mfa-enrolled-banner">
                <svg width="14" height="14" viewBox="0 0 14 14" fill="none" style={{ flexShrink: 0 }}>
                  <path d="M2 7l3.5 3.5L12 3" stroke="#22c55e" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
                </svg>
                Two-factor authentication is now active on your account.
              </div>

              <button onClick={handleFinish} className="mfa-btn-primary" style={{ marginTop: '1.5rem' }}>
                Continue to account security
              </button>
            </div>
          )}
        </div>
      </div>
    </>
  )
}

// ── Scoped CSS ────────────────────────────────────────────────────────────────

const MFA_STYLES = `
  @import url('https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&family=IBM+Plex+Sans:wght@400;500;600&display=swap');

  .mfa-shell {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 2rem 1rem;
    background: #0a0f1a;
    position: relative;
    overflow: hidden;
    font-family: 'IBM Plex Sans', system-ui, sans-serif;
  }

  .mfa-bg-grid {
    position: absolute;
    inset: 0;
    background-image:
      linear-gradient(rgba(245,158,11,0.03) 1px, transparent 1px),
      linear-gradient(90deg, rgba(245,158,11,0.03) 1px, transparent 1px);
    background-size: 32px 32px;
    pointer-events: none;
  }

  .mfa-card {
    position: relative;
    width: 100%;
    max-width: 520px;
    background: #0f1629;
    border: 1px solid rgba(245,158,11,0.18);
    border-radius: 4px;
    padding: 2.5rem 2.5rem;
    box-shadow:
      0 0 0 1px rgba(245,158,11,0.06),
      0 32px 64px rgba(0,0,0,0.5),
      0 0 80px rgba(245,158,11,0.04) inset;
  }

  /* Corner accents */
  .mfa-card::before,
  .mfa-card::after {
    content: '';
    position: absolute;
    width: 16px;
    height: 16px;
    border-color: rgba(245,158,11,0.5);
    border-style: solid;
  }
  .mfa-card::before {
    top: -1px; left: -1px;
    border-width: 2px 0 0 2px;
  }
  .mfa-card::after {
    bottom: -1px; right: -1px;
    border-width: 0 2px 2px 0;
  }

  .mfa-header {
    display: flex;
    align-items: flex-start;
    gap: 1rem;
    margin-bottom: 1.75rem;
  }

  .mfa-shield-icon {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .mfa-title {
    font-size: 1.1rem;
    font-weight: 600;
    color: #f1f5f9;
    letter-spacing: -0.01em;
    margin: 0 0 0.2rem;
    line-height: 1.3;
  }

  .mfa-subtitle {
    font-size: 0.8rem;
    color: #64748b;
    margin: 0;
  }

  /* ── Step indicator ── */
  .mfa-steps {
    display: flex;
    align-items: center;
    gap: 0;
    margin-bottom: 2rem;
    padding: 1rem 1.25rem;
    background: rgba(255,255,255,0.02);
    border: 1px solid rgba(255,255,255,0.05);
    border-radius: 3px;
  }

  .mfa-step-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex: 1;
  }

  .mfa-step-circle {
    width: 26px;
    height: 26px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.7rem;
    font-weight: 600;
    font-family: 'IBM Plex Mono', monospace;
    flex-shrink: 0;
    transition: all 0.2s;
  }

  .mfa-step-circle.idle {
    border: 1px solid rgba(255,255,255,0.1);
    color: #475569;
    background: transparent;
  }

  .mfa-step-circle.active {
    border: 1px solid #f59e0b;
    color: #f59e0b;
    background: rgba(245,158,11,0.1);
    box-shadow: 0 0 12px rgba(245,158,11,0.2);
  }

  .mfa-step-circle.done {
    border: 1px solid #22c55e;
    color: #22c55e;
    background: rgba(34,197,94,0.1);
  }

  .mfa-step-label {
    font-size: 0.7rem;
    color: #475569;
    white-space: nowrap;
  }

  .mfa-step-label.active {
    color: #f59e0b;
  }

  .mfa-step-line {
    flex: 1;
    height: 1px;
    background: rgba(255,255,255,0.06);
    margin: 0 0.5rem;
    min-width: 20px;
    transition: background 0.2s;
  }

  .mfa-step-line.done {
    background: rgba(34,197,94,0.3);
  }

  /* ── Error ── */
  .mfa-error {
    display: flex;
    align-items: flex-start;
    gap: 0.5rem;
    font-size: 0.8rem;
    color: #f87171;
    background: rgba(239,68,68,0.08);
    border: 1px solid rgba(239,68,68,0.2);
    border-radius: 3px;
    padding: 0.625rem 0.875rem;
    margin-bottom: 1.25rem;
    line-height: 1.4;
  }

  /* ── Loading state ── */
  .mfa-loading-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1rem;
    padding: 3rem 0;
    color: #475569;
    font-size: 0.85rem;
  }

  .mfa-spinner {
    width: 32px;
    height: 32px;
    border: 2px solid rgba(245,158,11,0.15);
    border-top-color: #f59e0b;
    border-radius: 50%;
    animation: mfa-spin 0.8s linear infinite;
  }

  .mfa-spinner-sm {
    width: 14px;
    height: 14px;
    border: 1.5px solid rgba(245,158,11,0.2);
    border-top-color: #f59e0b;
    border-radius: 50%;
    animation: mfa-spin 0.8s linear infinite;
    flex-shrink: 0;
  }

  @keyframes mfa-spin {
    to { transform: rotate(360deg); }
  }

  /* ── Step content ── */
  .mfa-step-content {
    display: flex;
    flex-direction: column;
  }

  .mfa-instruction {
    display: flex;
    gap: 0.75rem;
    margin-bottom: 1rem;
  }

  .mfa-instruction-num {
    flex-shrink: 0;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    border: 1px solid rgba(245,158,11,0.3);
    color: #f59e0b;
    font-size: 0.65rem;
    font-weight: 700;
    font-family: 'IBM Plex Mono', monospace;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-top: 1px;
  }

  .mfa-instruction strong {
    display: block;
    font-size: 0.82rem;
    color: #cbd5e1;
    font-weight: 500;
    margin-bottom: 0.2rem;
  }

  .mfa-instruction p {
    font-size: 0.75rem;
    color: #475569;
    margin: 0;
    line-height: 1.5;
  }

  /* ── Secret display ── */
  .mfa-secret-block {
    background: #070c16;
    border: 1px solid rgba(245,158,11,0.15);
    border-radius: 3px;
    padding: 1rem 1.125rem;
    margin: 1rem 0;
  }

  .mfa-secret-label {
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: #334155;
    font-family: 'IBM Plex Mono', monospace;
    margin-bottom: 0.4rem;
  }

  .mfa-secret-value {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    justify-content: space-between;
  }

  .mfa-secret-value code {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.9rem;
    color: #f59e0b;
    letter-spacing: 0.05em;
    word-break: break-all;
    line-height: 1.6;
  }

  .mfa-copy-btn {
    flex-shrink: 0;
    background: transparent;
    border: 1px solid rgba(255,255,255,0.08);
    border-radius: 3px;
    padding: 0.3rem;
    color: #475569;
    cursor: pointer;
    display: flex;
    align-items: center;
    transition: color 0.15s, border-color 0.15s;
  }

  .mfa-copy-btn:hover {
    color: #f59e0b;
    border-color: rgba(245,158,11,0.3);
  }

  .mfa-uri-block {
    margin-top: 0.75rem;
    padding-top: 0.75rem;
    border-top: 1px solid rgba(255,255,255,0.05);
  }

  .mfa-uri-text {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.65rem;
    color: #334155;
    word-break: break-all;
    line-height: 1.6;
    user-select: all;
  }

  /* ── OTP input ── */
  .otp-grid {
    display: flex;
    gap: 0.5rem;
    justify-content: center;
    margin: 1.25rem 0 0.5rem;
  }

  .otp-cell {
    width: 48px;
    height: 56px;
    background: #070c16;
    border: 1px solid rgba(255,255,255,0.08);
    border-radius: 3px;
    color: #f1f5f9;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 1.4rem;
    font-weight: 500;
    text-align: center;
    outline: none;
    transition: border-color 0.15s, box-shadow 0.15s;
    caret-color: #f59e0b;
  }

  .otp-cell:focus {
    border-color: rgba(245,158,11,0.5);
    box-shadow: 0 0 0 3px rgba(245,158,11,0.08);
  }

  .otp-cell.filled {
    border-color: rgba(245,158,11,0.25);
    color: #f59e0b;
  }

  /* ── Verifying indicator ── */
  .mfa-verifying {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.78rem;
    color: #64748b;
    justify-content: center;
    margin-top: 0.75rem;
  }

  /* ── Recovery codes ── */
  .recovery-block {
    margin: 1rem 0;
  }

  .recovery-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.375rem;
    background: #070c16;
    border: 1px solid rgba(245,158,11,0.12);
    border-radius: 3px;
    padding: 1rem;
    margin-bottom: 0.875rem;
  }

  .recovery-code-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.3rem 0.5rem;
    border-radius: 2px;
    background: rgba(255,255,255,0.02);
  }

  .recovery-code-num {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.6rem;
    color: #334155;
    min-width: 16px;
  }

  .recovery-code-val {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.8rem;
    color: #94a3b8;
    letter-spacing: 0.04em;
  }

  .recovery-actions {
    display: flex;
    gap: 0.5rem;
  }

  /* ── Recovery notice ── */
  .mfa-recovery-notice {
    display: flex;
    gap: 0.75rem;
    align-items: flex-start;
    background: rgba(245,158,11,0.06);
    border: 1px solid rgba(245,158,11,0.18);
    border-radius: 3px;
    padding: 0.875rem 1rem;
    margin-bottom: 1.25rem;
    font-size: 0.8rem;
    color: #94a3b8;
    line-height: 1.5;
  }

  .mfa-recovery-notice strong {
    display: block;
    color: #f59e0b;
    font-size: 0.8rem;
    margin-bottom: 0.3rem;
  }

  .mfa-recovery-notice p {
    margin: 0;
  }

  /* ── Enrolled success banner ── */
  .mfa-enrolled-banner {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.8rem;
    color: #4ade80;
    background: rgba(34,197,94,0.06);
    border: 1px solid rgba(34,197,94,0.15);
    border-radius: 3px;
    padding: 0.625rem 0.875rem;
    margin-top: 1rem;
  }

  /* ── Buttons ── */
  .mfa-btn-primary {
    width: 100%;
    padding: 0.625rem 1rem;
    background: #f59e0b;
    color: #000;
    font-size: 0.82rem;
    font-weight: 600;
    font-family: 'IBM Plex Sans', system-ui, sans-serif;
    border: none;
    border-radius: 3px;
    cursor: pointer;
    transition: background 0.15s, opacity 0.15s;
    letter-spacing: 0.01em;
  }

  .mfa-btn-primary:hover {
    background: #fbbf24;
  }

  .mfa-btn-primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .mfa-btn-secondary {
    flex: 1;
    padding: 0.5rem 0.875rem;
    background: transparent;
    color: #64748b;
    font-size: 0.75rem;
    font-family: 'IBM Plex Sans', system-ui, sans-serif;
    border: 1px solid rgba(255,255,255,0.08);
    border-radius: 3px;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
    letter-spacing: 0.01em;
  }

  .mfa-btn-secondary:hover {
    color: #cbd5e1;
    border-color: rgba(255,255,255,0.16);
  }
`
