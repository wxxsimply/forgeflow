import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { confirmMFA, createUserDataExport, deleteCurrentAccount, downloadUserDataExport, getMFAStatus, setupMFA, type User } from '../api/client';
import { useAuth } from '../auth/AuthProvider';

export function AccountPage() {
  const { user } = useAuth();
  const queryClient = useQueryClient();
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [mfaPassword, setMFAPassword] = useState('');
  const [mfaCode, setMFACode] = useState('');
  const mfaStatus = useQuery({ queryKey: ['account', 'mfa'], queryFn: getMFAStatus, enabled: user?.role === 'admin', retry: false });
  const mfaRequired = Boolean(user?.mfaRequired || mfaStatus.data?.required);
  const mfaSetup = useMutation({ mutationFn: () => setupMFA(mfaPassword) });
  const mfaConfirm = useMutation({
    mutationFn: () => confirmMFA(mfaCode),
    onSuccess: () => {
      queryClient.setQueryData<User | null>(['auth', 'me'], (current) => current ? { ...current, mfaEnabled: true, mfaRequired: false } : current);
      void mfaStatus.refetch();
    },
  });
  const exportMutation = useMutation({
    mutationFn: async () => {
      const ticket = await createUserDataExport();
      await downloadUserDataExport(ticket.id);
      return ticket;
    },
  });
  const deletion = useMutation({
    mutationFn: () => deleteCurrentAccount(password),
    onSuccess: () => window.location.assign('/login'),
  });

  function submitDeletion(event: FormEvent) {
    event.preventDefault();
    deletion.mutate();
  }

  return <div className="page">
    <div className="page-heading"><div><span className="eyebrow">数据与隐私</span><h1>数据与账户</h1><p>下载属于你的 ForgeFlow 数据，或发起可审计、可恢复的账户删除。</p></div></div>
    <div className="account-grid">
      {user?.role === 'admin' && <section className="account-card">
        <span className="eyebrow">管理员安全</span>
        <h2>多因素认证</h2>
        {mfaStatus.data?.enabled ? <p className="form-success" role="status">TOTP MFA 已启用。登录时必须输入动态验证码或一次性恢复码。</p> : <>
          <p>{mfaRequired ? '当前会话仅可完成 MFA 绑定；绑定前不能访问控制面功能。' : '绑定身份验证器后，管理员登录将始终要求第二因子。'}</p>
          {!mfaSetup.data && <form className="account-delete-form" onSubmit={(event) => { event.preventDefault(); mfaSetup.mutate(); }}>
            <label htmlFor="mfa-password">当前密码</label>
            <input id="mfa-password" type="password" autoComplete="current-password" value={mfaPassword} onChange={(event) => setMFAPassword(event.target.value)} required />
            <button className="secondary-button" type="submit" disabled={mfaSetup.isPending || !mfaPassword}>{mfaSetup.isPending ? '正在准备…' : '开始绑定'}</button>
            {mfaSetup.isError && <p className="form-error" role="alert">无法开始绑定，请确认密码与服务器 MFA Secret 配置。</p>}
          </form>}
          {mfaSetup.data && !mfaConfirm.data && <form className="account-delete-form" onSubmit={(event) => { event.preventDefault(); mfaConfirm.mutate(); }}>
            <p>在身份验证器中手工输入以下密钥；绑定信息将在 {new Date(mfaSetup.data.expiresAt).toLocaleString('zh-CN')} 失效。</p>
            <code className="mfa-secret">{mfaSetup.data.secret}</code>
            <label htmlFor="mfa-code">6 位动态验证码</label>
            <input id="mfa-code" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" value={mfaCode} onChange={(event) => setMFACode(event.target.value)} required />
            <button className="primary-button" type="submit" disabled={mfaConfirm.isPending || !/^[0-9]{6}$/.test(mfaCode)}>{mfaConfirm.isPending ? '正在确认…' : '确认并启用'}</button>
            {mfaConfirm.isError && <p className="form-error" role="alert">验证码错误、已使用或绑定已过期，请重试。</p>}
          </form>}
          {mfaConfirm.data && <div className="mfa-recovery" role="status">
            <strong>请立即离线保存这些一次性恢复码</strong>
            <p>恢复码只显示这一次；每个只能使用一次，不能提交到 Git。</p>
            <div className="recovery-code-grid">{mfaConfirm.data.recoveryCodes.map((code) => <code key={code}>{code}</code>)}</div>
          </div>}
        </>}
      </section>}
      {!mfaRequired && <section className="account-card">
        <span className="eyebrow">数据导出</span>
        <h2>下载我的数据</h2>
        <p>生成一个短时有效的 ZIP。压缩包包含数据库记录和经过校验的任务产物，不包含密码、会话令牌或请求摘要。</p>
        <button className="secondary-button" type="button" disabled={exportMutation.isPending} onClick={() => exportMutation.mutate()}>
          {exportMutation.isPending ? '正在生成…' : '生成并下载'}
        </button>
        {exportMutation.isSuccess && <p className="form-success" role="status">导出已开始下载；下载凭证将在短时间后失效。</p>}
        {exportMutation.isError && <p className="form-error" role="alert">导出失败，请稍后重试。</p>}
      </section>}
      {!mfaRequired && <section className="account-card account-card--danger">
        <span className="eyebrow">危险操作</span>
        <h2>永久删除账户</h2>
        <p>提交后会立即注销全部会话并停止新的任务。后台将重试删除任务、产物和账户记录；最后一个有效管理员不能删除自己。</p>
        <form className="account-delete-form" onSubmit={submitDeletion}>
          <label htmlFor="delete-password">当前密码</label>
          <input id="delete-password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required />
          <label htmlFor="delete-confirmation">输入 DELETE 确认</label>
          <input id="delete-confirmation" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} required />
          <button className="danger-button" type="submit" disabled={deletion.isPending || confirmation !== 'DELETE' || !password}>
            {deletion.isPending ? '正在提交…' : '删除我的账户'}
          </button>
          {deletion.isError && <p className="form-error" role="alert">删除请求未提交。请确认密码和确认词，或联系管理员。</p>}
        </form>
      </section>}
    </div>
  </div>;
}
