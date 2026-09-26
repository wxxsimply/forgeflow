import { useEffect, useId, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { APIError, registerAccount } from '../api/client';
import { useAuth } from '../auth/AuthProvider';

export function RegisterPage() {
  const { user, loading } = useAuth();
  const navigate = useNavigate();
  const errorId = useId();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!loading && user) navigate(user.mfaRequired && !user.mfaEnabled ? '/account' : '/runs', { replace: true });
  }, [loading, navigate, user]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    if (password !== confirmation) { setError('两次输入的密码不一致。'); return; }
    setError(''); setSubmitting(true);
    try {
      await registerAccount({ email: email.trim(), password });
      navigate('/login?registered=1', { replace: true });
    } catch (caught) {
      if (caught instanceof APIError && caught.status === 409) setError('该邮箱已注册，请直接登录。');
      else if (caught instanceof APIError && caught.status === 429) setError('注册尝试过于频繁，请稍后再试。');
      else if (caught instanceof APIError && caught.status === 400) setError('请检查邮箱格式，密码至少 12 个字符。');
      else setError('暂时无法注册，请检查网络后重试。');
    } finally { setSubmitting(false); }
  }

  return <main className="login-page">
    <section className="login-story" aria-label="ForgeFlow 产品介绍">
      <div className="brand brand--light"><span className="brand__mark" aria-hidden="true"><i /><i /><i /></span><span><strong>ForgeFlow</strong><small>可控交付</small></span></div>
      <div className="login-story__content">
        <span className="eyebrow">公开预览 · 模拟流程</span>
        <h1>创建账号，<br />开始审查式交付。</h1>
        <p>注册后可以为自己的受控仓库创建和审批模拟任务。网页不会调用真实模型或修改源码。</p>
      </div>
      <small className="login-story__foot">流程演示不等于生产自动执行</small>
    </section>
    <section className="login-panel">
      <form className="login-card" onSubmit={submit} aria-describedby={error ? errorId : undefined}>
        <span className="eyebrow">加入 ForgeFlow</span>
        <h2>创建账号</h2>
        <p className="login-card__intro">使用你自己的邮箱和密码；账号仅能访问自己的任务与仓库。</p>
        <label htmlFor="register-email">邮箱</label>
        <input id="register-email" type="email" name="email" autoComplete="email" required maxLength={320} value={email} onChange={(event) => setEmail(event.target.value)} />
        <label htmlFor="register-password">密码</label>
        <input id="register-password" type="password" name="password" autoComplete="new-password" required minLength={12} maxLength={1024} value={password} onChange={(event) => setPassword(event.target.value)} aria-describedby="register-password-hint" />
        <span id="register-password-hint" className="field-hint">至少 12 个字符。当前不提供密码找回，请妥善保存。</span>
        <label htmlFor="register-confirmation">确认密码</label>
        <input id="register-confirmation" type="password" name="confirmation" autoComplete="new-password" required minLength={12} maxLength={1024} value={confirmation} onChange={(event) => setConfirmation(event.target.value)} />
        {error && <p id={errorId} className="form-error" role="alert">{error}</p>}
        <button className="primary-button" type="submit" disabled={submitting}>{submitting ? '正在注册…' : '注册账号'}</button>
        <div className="login-help"><span>已有账号？ <Link to="/login">返回登录</Link></span></div>
      </form>
    </section>
  </main>;
}
