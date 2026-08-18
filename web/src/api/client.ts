import createClient from 'openapi-fetch';
import type { components, paths } from './schema';

export type User = components['schemas']['User'];
export type Session = components['schemas']['Session'];
export type Repository = components['schemas']['Repository'];
export type RepositoryPage = components['schemas']['RepositoryPage'];
export type Run = components['schemas']['Run'];
export type RunEvent = components['schemas']['RunEvent'];
export type SequencedEvent = components['schemas']['SequencedEvent'];
export type RunPage = components['schemas']['RunPage'];
export type Approval = components['schemas']['Approval'];
export type ApprovalList = components['schemas']['ApprovalList'];
export type Artifact = components['schemas']['Artifact'];
export type RunReport = components['schemas']['RunReport'];
export type EvalRun = components['schemas']['EvalRun'];
export type Agent = components['schemas']['Agent'];
export type Prompt = components['schemas']['Prompt'];
export type PromptRelease = components['schemas']['PromptRelease'];

type APIErrorBody = components['schemas']['Error'];
type ApprovalStatus = 'pending' | 'approved' | 'rejected';

export class APIError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId?: string;
  readonly details: Record<string, unknown>;

  constructor(status: number, body?: Partial<APIErrorBody>) {
    super(body?.message || fallbackMessage(status));
    this.name = 'APIError';
    this.status = status;
    this.code = body?.code || 'network_error';
    this.requestId = body?.requestId;
    this.details = body?.details || {};
  }
}

const client = createClient<paths>({ baseUrl: '/api/v1', credentials: 'include' });
let memoryCSRFToken = '';

client.use({
  async onRequest({ request }) {
    if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method.toUpperCase())) {
      const token = csrfToken();
      if (token) request.headers.set('X-CSRF-Token', token);
    }
    request.headers.set('X-Request-ID', crypto.randomUUID());
    return request;
  },
});

export async function login(input: { email: string; password: string; remember: boolean }): Promise<User> {
  const { data, error, response } = await client.POST('/auth/login', { body: input });
  if (!data) throw toAPIError(response, error);
  memoryCSRFToken = data.csrfToken;
  return data.user;
}

export async function getCurrentUser(): Promise<User> {
  const { data, error, response } = await client.GET('/auth/me');
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function logout(): Promise<void> {
  const { error, response } = await client.POST('/auth/logout', { params: { header: csrfHeader() } });
  if (!response.ok) throw toAPIError(response, error);
  memoryCSRFToken = '';
}

export async function listSessions(): Promise<components['schemas']['SessionList']> {
  const { data, error, response } = await client.GET('/auth/sessions');
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function revokeSession(sessionId: string): Promise<void> {
  const { error, response } = await client.DELETE('/auth/sessions/{sessionId}', { params: { path: { sessionId }, header: csrfHeader() } });
  if (!response.ok) throw toAPIError(response, error);
}

export async function listRepositories(cursor?: string): Promise<RepositoryPage> {
  const { data, error, response } = await client.GET('/repositories', { params: { query: { cursor, limit: 100 } } });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function createRepository(input: { name: string; localPath: string; defaultBranch?: string }): Promise<Repository> {
  const { data, error, response } = await client.POST('/repositories', { params: { header: csrfHeader() }, body: { ...input, defaultBranch: input.defaultBranch || 'HEAD' } });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function listRuns(cursor?: string): Promise<RunPage> {
  const { data, error, response } = await client.GET('/runs', { params: { query: { cursor, limit: 20 } } });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function createRun(input: { repositoryId: string; task: string; baseRevision?: string; maxIterations?: number }, idempotencyKey: string): Promise<Run> {
  const { data, error, response } = await client.POST('/runs', {
    params: { header: { ...csrfHeader(), 'Idempotency-Key': idempotencyKey } },
    body: input,
  });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function getRun(runId: string): Promise<Run> {
  const { data, error, response } = await client.GET('/runs/{runId}', { params: { path: { runId } } });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function mutateRun(runId: string, action: 'pause' | 'resume' | 'cancel', reason = ''): Promise<Run> {
  if (action === 'resume') {
    const { data, error, response } = await client.POST('/runs/{runId}/resume', { params: { path: { runId }, header: csrfHeader() } });
    if (!data) throw toAPIError(response, error);
    return data;
  }
  const path = action === 'pause' ? '/runs/{runId}/pause' : '/runs/{runId}/cancel';
  const { data, error, response } = await client.POST(path, { params: { path: { runId }, header: csrfHeader() }, body: { reason } });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function listRunEvents(runId: string, after = 0): Promise<components['schemas']['EventPage']> {
  const { data, error, response } = await client.GET('/runs/{runId}/events', { params: { path: { runId }, query: { after, limit: 200 } } });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function listApprovals(status?: ApprovalStatus): Promise<ApprovalList> {
  const { data, error, response } = await client.GET('/approvals', { params: { query: { status } } });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function getApproval(approvalId: string): Promise<{ approval: Approval; etag: string }> {
  const { data, error, response } = await client.GET('/approvals/{approvalId}', { params: { path: { approvalId } } });
  if (!data) throw toAPIError(response, error);
  return { approval: data, etag: response.headers.get('ETag') || `"${data.runVersion}"` };
}

export async function decideApproval(approvalId: string, etag: string, decision: 'approve' | 'reject', comment: string): Promise<Run> {
  const { data, error, response } = await client.POST('/approvals/{approvalId}/decision', {
    params: { path: { approvalId }, header: { ...csrfHeader(), 'If-Match': etag } },
    body: { decision, comment },
  });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function listRunArtifacts(runId: string): Promise<components['schemas']['ArtifactList']> {
  const { data, error, response } = await client.GET('/runs/{runId}/artifacts', { params: { path: { runId } } });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function getRunReport(runId: string): Promise<RunReport> {
  const { data, error, response } = await client.GET('/runs/{runId}/report', { params: { path: { runId } } });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function listEvalRuns(): Promise<components['schemas']['EvalRunList']> {
  const { data, error, response } = await client.GET('/evals/runs', { params: { query: { limit: 50 } } });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function getEvalRun(evalRunId: string): Promise<EvalRun> {
  const { data, error, response } = await client.GET('/evals/runs/{evalRunId}', { params: { path: { evalRunId } } });
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function listAgents(): Promise<components['schemas']['AgentList']> {
  const { data, error, response } = await client.GET('/agents');
  if (!data) throw toAPIError(response, error);
  return data;
}

export async function listPrompts(): Promise<components['schemas']['PromptCatalog']> {
  const { data, error, response } = await client.GET('/prompts');
  if (!data) throw toAPIError(response, error);
  return data;
}

export function runEventStreamURL(runId: string, after: number): string {
  const query = after > 0 ? `?after=${encodeURIComponent(after)}` : '';
  return `/api/v1/runs/${encodeURIComponent(runId)}/stream${query}`;
}

function csrfHeader(): { 'X-CSRF-Token': string } { return { 'X-CSRF-Token': csrfToken() }; }
function csrfToken(): string {
  if (memoryCSRFToken) return memoryCSRFToken;
  const prefix = 'forgeflow_csrf=';
  const value = document.cookie.split(';').map((part) => part.trim()).find((part) => part.startsWith(prefix));
  return value ? decodeURIComponent(value.slice(prefix.length)) : '';
}

function toAPIError(response: Response, body: unknown): APIError {
  if (body && typeof body === 'object') return new APIError(response.status, body as Partial<APIErrorBody>);
  return new APIError(response.status);
}

function fallbackMessage(status: number): string {
  if (status === 401) return '登录状态已失效，请重新登录。';
  if (status === 403) return '你没有权限执行此操作。';
  if (status === 404) return '请求的资源不存在。';
  if (status === 409) return '资源已被其他操作更新，请刷新后重试。';
  if (status === 429) return '请求过于频繁，请稍后再试。';
  if (status >= 500) return '服务暂时不可用，请稍后重试。';
  return '请求失败，请重试。';
}
