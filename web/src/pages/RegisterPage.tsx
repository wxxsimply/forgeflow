import { useEffect, useId, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { APIError, registerAccount, requestRegistrationCode } from '../api/client';
import { useAuth } from '../auth/AuthProvider';

export function RegisterPage() {
  const { user, loading } = useAuth();
  const navigate = useNavigate();
  const errorId = useId();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [code, setCode] = useState('');
  const [sendingCode, setSendingCode] = useState(false);
  const [codeSent, setCodeSent] = useState(false);
  const [resendSeconds, setResendSeconds] = useState(0);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!loading && user) navigate(user.mfaRequired && !user.mfaEnabled ? '/account' : '/runs', { replace: true });
  }, [loading, navigate, user]);

  useEffect(() => {
    if (resendSeconds <= 0) return;
    const timer = window.setTimeout(() => setResendSeconds((seconds) => seconds - 1), 1000);
    return () => window.clearTimeout(timer);
  }, [resendSeconds]);

  async function sendCode() {
    if (sendingCode || resendSeconds > 0 || !email.trim()) return;
    setError(''); setSendingCode(true);
    try {
      await requestRegistrationCode(email.trim());
      setCodeSent(true);
      setResendSeconds(60);
    } catch (caught) {
      if (caught instanceof APIError && caught.status === 429) setError('验证码请求过于频繁，请稍后重试。');
      else if (caught instanceof APIError && caught.status === 400) setError('请输入有效的邮箱地址。');
      else setError('暂时无法发送验证码，请稍后重试或联系管理员。');
    } finally { setSendingCode(false); }
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    if (password !== confirmation) { setError('两次输入的密码不一致。'); return; }
    if (!/^[0-9]{8}$/.test(code)) { setError('请输入邮件中的 8 位验证码。'); return; }
    setError(''); setSubmitting(true);
    try {
      await registerAccount({ email: email.trim(), password, code });
      navigate('/login?registered=1', { replace: true });
    } catch (caught) {
      if (caught instanceof APIError && caught.status === 409) setError('该邮箱已注册，请直接登录。');
      else if (caught instanceof APIError && caught.status === 429) setError('注册尝试过于频繁，请稍后再试。');
      else if (caught instanceof APIError && caught.status === 400) setError('验证码错误或已过期；请检查邮箱、密码和验证码后重试。');
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
        <p className="login-card__intro">使用自己的邮箱接收验证码。验证成功后，账号仅能访问自己的任务与仓库。</p>
        <label htmlFor="register-email">邮箱</label>
        <input id="register-email" type="email" name="email" autoComplete="email" required maxLength={320} value={email} onChange={(event) => { setEmail(event.target.value); setCodeSent(false); setCode(''); }} />
        <div className="registration-code-row">
          <div><label htmlFor="register-code">邮箱验证码</label><input id="register-code" type="text" name="code" autoComplete="one-time-code" inputMode="numeric" pattern="[0-9]{8}" maxLength={8} required value={code} onChange={(event) => setCode(event.target.value.replace(/[^0-9]/g, ''))} aria-describedby="register-code-hint" /></div>
          <button type="button" className="secondary-button" disabled={sendingCode || resendSeconds > 0 || !email.trim()} onClick={sendCode}>{sendingCode ? '发送中…' : resendSeconds > 0 ? `${resendSeconds} 秒后重发` : '获取验证码'}</button>
        </div>
        <span id="register-code-hint" className="field-hint">{codeSent ? '验证码已发送，请查收邮箱（也可检查垃圾邮件）。' : '验证码 10 分钟内有效，发送间隔至少 1 分钟。'}</span>
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
