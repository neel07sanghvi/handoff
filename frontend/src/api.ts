const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8080'

export type User = {
  id: string
  workspace_id: string
  name: string
  email: string
  role: string
  created_at: string
}

export type Workspace = {
  id: string
  name: string
  slug: string
  created_at: string
}

export type AuthResponse = {
  token: string
  user: User
  workspace: Workspace
}

export type MeResponse = {
  user: User
  workspace: Workspace
}

export type Ticket = {
  id: string
  workspace_id: string
  external_id: string | null
  title: string
  description: string | null
  url: string | null
  source: string
  created_at: string
  updated_at: string
}

export type Session = {
  id: string
  ticket_id: string
  user_id: string
  title: string
  goal: string | null
  status: string
  summary: string | null
  created_at: string
  updated_at: string
  completed_at: string | null
}

export type Entry = {
  id: string
  session_id: string
  user_id: string
  type: string
  content: string
  created_at: string
  updated_at: string
}

export type HandoffResponse = {
  ticket_id: string
  content: string
}

export type SearchResult = {
  id: string
  session_id: string
  ticket_id: string
  ticket_external_id: string | null
  ticket_title: string
  user_id: string
  user_name: string
  type: string
  content: string
  created_at: string
  updated_at: string
}

type RequestOptions = {
  method?: string
  token?: string
  body?: unknown
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
  }

  if (options.token) {
    headers.Authorization = `Bearer ${options.token}`
  }

  const response = await fetch(`${API_BASE}${path}`, {
    method: options.method ?? 'GET',
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  })

  if (response.status === 204) {
    return undefined as T
  }

  const text = await response.text()
  const data = text ? JSON.parse(text) : null

  if (!response.ok) {
    throw new Error(data?.error ?? 'Request failed')
  }

  return data as T
}

export function register(body: {
  name: string
  email: string
  password: string
  workspace_name: string
}) {
  return request<AuthResponse>('/api/auth/register', { method: 'POST', body })
}

export function login(body: { email: string; password: string }) {
  return request<AuthResponse>('/api/auth/login', { method: 'POST', body })
}

export function me(token: string) {
  return request<MeResponse>('/api/me', { token })
}

export function listTickets(token: string) {
  return request<Ticket[]>('/api/tickets', { token })
}

export function createTicket(
  token: string,
  body: {
    external_id?: string
    title: string
    description?: string
    url?: string
    source: string
  },
) {
  return request<Ticket>('/api/tickets', { method: 'POST', token, body })
}

export function listSessions(token: string, ticketID: string) {
  return request<Session[]>(`/api/tickets/${ticketID}/sessions`, { token })
}

export function createSession(
  token: string,
  ticketID: string,
  body: { title: string; goal?: string },
) {
  return request<Session>(`/api/tickets/${ticketID}/sessions`, { method: 'POST', token, body })
}

export function patchSession(
  token: string,
  sessionID: string,
  body: { status?: string; summary?: string },
) {
  return request<Session>(`/api/sessions/${sessionID}`, { method: 'PATCH', token, body })
}

export function listEntries(token: string, sessionID: string) {
  return request<Entry[]>(`/api/sessions/${sessionID}/entries`, { token })
}

export function createEntry(
  token: string,
  sessionID: string,
  body: { type: string; content: string },
) {
  return request<Entry>(`/api/sessions/${sessionID}/entries`, { method: 'POST', token, body })
}

export function patchEntry(
  token: string,
  entryID: string,
  body: { type?: string; content?: string },
) {
  return request<Entry>(`/api/entries/${entryID}`, { method: 'PATCH', token, body })
}

export function deleteEntry(token: string, entryID: string) {
  return request<void>(`/api/entries/${entryID}`, { method: 'DELETE', token })
}

export function getHandoff(token: string, ticketID: string) {
  return request<HandoffResponse>(`/api/tickets/${ticketID}/handoff`, { token })
}

export function searchEntries(token: string, query: string, type: string) {
  const params = new URLSearchParams({ q: query })
  if (type) {
    params.set('type', type)
  }

  return request<SearchResult[]>(`/api/search?${params.toString()}`, { token })
}
