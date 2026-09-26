import { useEffect, useId, useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
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
  const [secondFactor, setSecondFactor] = useState('');
  const [remember, setRemember] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const destination = safeDestination(new URLSearchParams(location.search).get('next'));
  const registered = new URLSearchParams(location.search).get('registered') === '1';
  useEffect(() => {
    if (!loading && user) navigate(user.mfaRequired && !user.mfaEnabled ? '/account' : '/runs', { replace: true });
  }, [loading, navigate, user]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    setError(''); setSubmitting(true);
    try {
      const signedIn = await signIn({ email, password, secondFactor: secondFactor.trim() || undefined, remember });
      navigate(signedIn.mfaRequired && !signedIn.mfaEnabled ? '/account' : destination, { replace: true });
    } catch (caught) {
      if (caught instanceof APIError && caught.status === 401) setError('邮箱、密码或管理员验证码错误。');
      else if (caught instanceof APIError && caught.status === 429) setError('尝试次数过多，请稍后再试。');
      else setError('暂时无法连接 ForgeFlow，请检查网络后重试。');
    } finally { setSubmitting(false); }
  }

  return (
    <main className="login-page">
      <section className="login-story" aria-label="ForgeFlow 产品介绍">
        <div className="brand brand--light"><span className="brand__mark" aria-hidden="true"><i /><i /><i /></span><span><strong>ForgeFlow</strong><small>可控交付</small></span></div>
        <div className="login-story__content">
          <span className="eyebrow">公开预览 · 模拟流程</span>
          <h1>把任务放进<br />可审查、可恢复的流程。</h1>
          <p>当前网页展示模拟（Mock）规划、人工审批和审计记录，不会从网页调用真实模型或修改源码。</p>
          <div className="flow-line" aria-hidden="true"><i className="done" /><span /><i className="done" /><span /><i className="active" /><span /><i /></div>
          <div className="flow-labels" aria-hidden="true"><span>任务</span><span>规划</span><span>审批</span><span>记录</span></div>
        </div>
        <small className="login-story__foot">流程演示不等于生产自动执行</small>
      </section>
      <section className="login-panel">
        <form className="login-card" onSubmit={submit} aria-describedby={error ? formErrorId : undefined}>
          <span className="eyebrow">欢迎回来</span>
          <h2>登录控制台</h2>
          <p className="login-card__intro">使用你的账号继续；没有账号也可以自行注册。</p>
          {registered && <p className="form-success" role="status">注册成功，请使用新账号登录。</p>}
          <label htmlFor="email">邮箱</label>
          <input id="email" name="email" type="email" autoComplete="username" required value={email} onChange={(event) => setEmail(event.target.value)} aria-describedby={emailErrorId} />
          <span id={emailErrorId} className="field-hint">请输入你的账号邮箱。</span>
          <label htmlFor="password">密码</label>
          <input id="password" name="password" type="password" autoComplete="current-password" required minLength={12} value={password} onChange={(event) => setPassword(event.target.value)} />
          <label htmlFor="second-factor">管理员验证码或恢复码</label>
          <input id="second-factor" name="secondFactor" autoComplete="one-time-code" autoCapitalize="characters" value={secondFactor} onChange={(event) => setSecondFactor(event.target.value)} aria-describedby="second-factor-hint" />
          <span id="second-factor-hint" className="field-hint">管理员启用 MFA 后必填；普通账号请留空。</span>
          <label className="checkbox-row"><input type="checkbox" checked={remember} onChange={(event) => setRemember(event.target.checked)} /><span>在这台设备上保持登录</span></label>
          {error && <p id={formErrorId} className="form-error" role="alert">{error}</p>}
          <button className="primary-button" type="submit" disabled={submitting}>
            {submitting ? <><span className="spinner spinner--small" />正在验证</> : '登录'}
          </button>
          <div className="login-help"><span>还没有账号？ <Link to="/register">创建账号</Link></span><span>无法登录请联系管理员</span></div>
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
