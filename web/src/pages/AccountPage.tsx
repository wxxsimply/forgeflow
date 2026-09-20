import { useState, type FormEvent } from 'react';
import { useMutation } from '@tanstack/react-query';
import { createUserDataExport, deleteCurrentAccount, downloadUserDataExport } from '../api/client';

export function AccountPage() {
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
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
      <section className="account-card">
        <span className="eyebrow">数据导出</span>
        <h2>下载我的数据</h2>
        <p>生成一个短时有效的 ZIP。压缩包包含数据库记录和经过校验的任务产物，不包含密码、会话令牌或请求摘要。</p>
        <button className="secondary-button" type="button" disabled={exportMutation.isPending} onClick={() => exportMutation.mutate()}>
          {exportMutation.isPending ? '正在生成…' : '生成并下载'}
        </button>
        {exportMutation.isSuccess && <p className="form-success" role="status">导出已开始下载；下载凭证将在短时间后失效。</p>}
        {exportMutation.isError && <p className="form-error" role="alert">导出失败，请稍后重试。</p>}
      </section>
      <section className="account-card account-card--danger">
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
      </section>
    </div>
  </div>;
}
