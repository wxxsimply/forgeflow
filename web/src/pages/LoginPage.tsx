import { useEffect, useId, useState, type FormEvent } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { APIError } from '../api/client';
import { useAuth } from '../auth/AuthProvider';

export function LoginPage() {
  const { user, loading, signIn } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const emailErrorId = useId();
  const formErrorId = useId();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [remember, setRemember] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const destination = safeDestination(new URLSearchParams(location.search).get('next'));
  useEffect(() => { if (!loading && user) navigate('/runs', { replace: true }); }, [loading, navigate, user]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    setError(''); setSubmitting(true);
    try {
      await signIn({ email, password, remember });
      navigate(destination, { replace: true });
    } catch (caught) {
      if (caught instanceof APIError && caught.status === 401) setError('邮箱或密码错误。');
      else if (caught instanceof APIError && caught.status === 429) setError('尝试次数过多，请稍后再试。');
      else setError('暂时无法连接 ForgeFlow，请检查网络后重试。');
    } finally { setSubmitting(false); }
  }

  return (
    <main className="login-page">
      <section className="login-story" aria-label="ForgeFlow 产品介绍">
        <div className="brand brand--light"><span className="brand__mark" aria-hidden="true"><i /><i /><i /></span><span><strong>ForgeFlow</strong><small>Governed delivery</small></span></div>
        <div className="login-story__content">
          <span className="eyebrow">Controlled automation</span>
          <h1>Every agent action.<br />Visible and governed.</h1>
          <p>从计划到审查，所有执行都经过策略、证据和人工门禁。</p>
          <div className="flow-line" aria-hidden="true"><i className="done" /><span /><i className="done" /><span /><i className="active" /><span /><i /></div>
          <div className="flow-labels" aria-hidden="true"><span>Plan</span><span>Build</span><span>Review</span><span>Ship</span></div>
        </div>
        <small className="login-story__foot">Secure by design · Human in control</small>
      </section>
      <section className="login-panel">
        <form className="login-card" onSubmit={submit} aria-describedby={error ? formErrorId : undefined}>
          <span className="eyebrow">Welcome back</span>
          <h2>登录控制台</h2>
          <p className="login-card__intro">使用管理员分配的账号继续。</p>
          <label htmlFor="email">邮箱</label>
          <input id="email" name="email" type="email" autoComplete="username" required value={email} onChange={(event) => setEmail(event.target.value)} aria-describedby={emailErrorId} />
          <span id={emailErrorId} className="field-hint">请输入你的工作邮箱。</span>
          <label htmlFor="password">密码</label>
          <input id="password" name="password" type="password" autoComplete="current-password" required minLength={12} value={password} onChange={(event) => setPassword(event.target.value)} />
          <label className="checkbox-row"><input type="checkbox" checked={remember} onChange={(event) => setRemember(event.target.checked)} /><span>在这台设备上保持登录</span></label>
          {error && <p id={formErrorId} className="form-error" role="alert">{error}</p>}
          <button className="primary-button" type="submit" disabled={submitting}>
            {submitting ? <><span className="spinner spinner--small" />正在验证</> : '登录'}
          </button>
          <div className="login-help"><span>无法登录？</span><span>请联系 ForgeFlow 管理员</span></div>
        </form>
      </section>
    </main>
  );
}

export function safeDestination(value: string | null): string {
  if (!value || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')) return '/runs';
  try {
    const target = new URL(value, window.location.origin);
    return target.origin === window.location.origin ? `${target.pathname}${target.search}${target.hash}` : '/runs';
  } catch { return '/runs'; }
}
