<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import * as api from './api'

type Stage = 'tickets' | 'sessions' | 'entries' | 'handoff' | 'search'

const entryTypes = ['decision', 'tried_failed', 'bug_fixed', 'warning', 'prompt', 'context']
const ticketSources = ['manual', 'github', 'linear', 'jira']
const stages: Array<{ id: Stage; label: string }> = [
  { id: 'tickets', label: 'Tickets' },
  { id: 'sessions', label: 'Sessions' },
  { id: 'entries', label: 'Entries' },
  { id: 'handoff', label: 'Handoff' },
  { id: 'search', label: 'Search' },
]

const token = ref(localStorage.getItem('handoff_token') ?? '')
const user = ref<api.User | null>(null)
const workspace = ref<api.Workspace | null>(null)
const authMode = ref<'login' | 'register'>('login')
const activeStage = ref<Stage>('tickets')
const errorMessage = ref('')
const notice = ref('')
const loading = ref(false)

const tickets = ref<api.Ticket[]>([])
const sessions = ref<api.Session[]>([])
const entries = ref<api.Entry[]>([])
const searchResults = ref<api.SearchResult[]>([])
const handoffContent = ref('')
const selectedTicketID = ref('')
const selectedSessionID = ref('')
const editingEntryID = ref('')

const loginForm = reactive({
  email: '',
  password: '',
})

const registerForm = reactive({
  name: 'Neel',
  email: '',
  password: 'password123',
  workspace_name: 'Handoff',
})

const ticketForm = reactive({
  external_id: '',
  title: '',
  source: 'manual',
})

const sessionForm = reactive({
  title: '',
  goal: '',
})

const entryForm = reactive({
  type: 'decision',
  content: '',
})

const entryEditForm = reactive({
  type: 'decision',
  content: '',
})

const searchForm = reactive({
  q: '',
  type: '',
})

const selectedTicket = computed(() => tickets.value.find((ticket) => ticket.id === selectedTicketID.value))
const selectedSession = computed(() =>
  sessions.value.find((session) => session.id === selectedSessionID.value),
)
const isAuthenticated = computed(() => Boolean(token.value && user.value && workspace.value))

onMounted(async () => {
  if (!token.value) {
    return
  }

  try {
    const profile = await api.me(token.value)
    user.value = profile.user
    workspace.value = profile.workspace
    await loadTickets()
  } catch {
    logout()
  }
})

function setError(error: unknown) {
  errorMessage.value = error instanceof Error ? error.message : 'Something went wrong'
  notice.value = ''
}

function setNotice(message: string) {
  notice.value = message
  errorMessage.value = ''
}

function saveAuth(payload: api.AuthResponse) {
  token.value = payload.token
  user.value = payload.user
  workspace.value = payload.workspace
  localStorage.setItem('handoff_token', payload.token)
}

function logout() {
  token.value = ''
  user.value = null
  workspace.value = null
  tickets.value = []
  sessions.value = []
  entries.value = []
  searchResults.value = []
  selectedTicketID.value = ''
  selectedSessionID.value = ''
  handoffContent.value = ''
  activeStage.value = 'tickets'
  localStorage.removeItem('handoff_token')
}

function stageDisabled(stage: Stage) {
  if (stage === 'sessions' || stage === 'handoff') {
    return !selectedTicket.value
  }

  if (stage === 'entries') {
    return !selectedSession.value
  }

  return false
}

function setStage(stage: Stage) {
  if (!stageDisabled(stage)) {
    activeStage.value = stage
  }
}

async function handleLogin() {
  loading.value = true
  try {
    const payload = await api.login(loginForm)
    saveAuth(payload)
    await loadTickets()
    setNotice('Signed in')
  } catch (error) {
    setError(error)
  } finally {
    loading.value = false
  }
}

async function handleRegister() {
  loading.value = true
  try {
    const payload = await api.register(registerForm)
    saveAuth(payload)
    await loadTickets()
    setNotice('Workspace created')
  } catch (error) {
    setError(error)
  } finally {
    loading.value = false
  }
}

async function loadTickets() {
  if (!token.value) {
    return
  }

  tickets.value = await api.listTickets(token.value)
  if (tickets.value.length === 0) {
    selectedTicketID.value = ''
    selectedSessionID.value = ''
    sessions.value = []
    entries.value = []
    activeStage.value = 'tickets'
    return
  }

  const existingSelection = tickets.value.some((ticket) => ticket.id === selectedTicketID.value)
  await selectTicket(existingSelection ? selectedTicketID.value : tickets.value[0].id, false)
}

async function createTicket() {
  if (!token.value) {
    return
  }

  loading.value = true
  try {
    const ticket = await api.createTicket(token.value, {
      external_id: ticketForm.external_id || undefined,
      title: ticketForm.title,
      source: ticketForm.source,
    })
    tickets.value = [ticket, ...tickets.value]
    ticketForm.external_id = ''
    ticketForm.title = ''
    ticketForm.source = 'manual'
    await selectTicket(ticket.id)
    setNotice('Ticket created')
  } catch (error) {
    setError(error)
  } finally {
    loading.value = false
  }
}

async function selectTicket(ticketID: string, moveNext = true) {
  if (!token.value || !ticketID) {
    return
  }

  selectedTicketID.value = ticketID
  selectedSessionID.value = ''
  entries.value = []
  handoffContent.value = ''
  sessions.value = await api.listSessions(token.value, ticketID)

  if (sessions.value.length > 0) {
    await selectSession(sessions.value[0].id, false)
  }

  if (moveNext) {
    activeStage.value = 'sessions'
  }
}

async function createSession() {
  if (!token.value || !selectedTicket.value) {
    return
  }

  loading.value = true
  try {
    const session = await api.createSession(token.value, selectedTicket.value.id, {
      title: sessionForm.title,
      goal: sessionForm.goal || undefined,
    })
    sessions.value = [session, ...sessions.value]
    sessionForm.title = ''
    sessionForm.goal = ''
    await selectSession(session.id)
    setNotice('Session created')
  } catch (error) {
    setError(error)
  } finally {
    loading.value = false
  }
}

async function selectSession(sessionID: string, moveNext = true) {
  if (!token.value || !sessionID) {
    return
  }

  selectedSessionID.value = sessionID
  editingEntryID.value = ''
  entries.value = await api.listEntries(token.value, sessionID)

  if (moveNext) {
    activeStage.value = 'entries'
  }
}

async function completeSession() {
  if (!token.value || !selectedSession.value) {
    return
  }

  try {
    const updated = await api.patchSession(token.value, selectedSession.value.id, { status: 'completed' })
    sessions.value = sessions.value.map((session) => (session.id === updated.id ? updated : session))
    setNotice('Session completed')
  } catch (error) {
    setError(error)
  }
}

async function createEntry() {
  if (!token.value || !selectedSession.value) {
    return
  }

  loading.value = true
  try {
    const entry = await api.createEntry(token.value, selectedSession.value.id, entryForm)
    entries.value = [entry, ...entries.value]
    entryForm.type = 'decision'
    entryForm.content = ''
    setNotice('Entry added')
  } catch (error) {
    setError(error)
  } finally {
    loading.value = false
  }
}

function startEditEntry(entry: api.Entry) {
  editingEntryID.value = entry.id
  entryEditForm.type = entry.type
  entryEditForm.content = entry.content
}

async function saveEntryEdit() {
  if (!token.value || !editingEntryID.value) {
    return
  }

  try {
    const updated = await api.patchEntry(token.value, editingEntryID.value, {
      type: entryEditForm.type,
      content: entryEditForm.content,
    })
    entries.value = entries.value.map((entry) => (entry.id === updated.id ? updated : entry))
    editingEntryID.value = ''
    setNotice('Entry updated')
  } catch (error) {
    setError(error)
  }
}

async function deleteEntry(entryID: string) {
  if (!token.value) {
    return
  }

  try {
    await api.deleteEntry(token.value, entryID)
    entries.value = entries.value.filter((entry) => entry.id !== entryID)
    setNotice('Entry deleted')
  } catch (error) {
    setError(error)
  }
}

async function generateHandoff() {
  if (!token.value || !selectedTicket.value) {
    return
  }

  try {
    const handoff = await api.getHandoff(token.value, selectedTicket.value.id)
    handoffContent.value = handoff.content
    activeStage.value = 'handoff'
    setNotice('Handoff generated')
  } catch (error) {
    setError(error)
  }
}

async function copyHandoff() {
  if (!handoffContent.value) {
    return
  }

  await navigator.clipboard.writeText(handoffContent.value)
  setNotice('Copied')
}

async function runSearch() {
  if (!token.value) {
    return
  }

  try {
    searchResults.value = await api.searchEntries(token.value, searchForm.q, searchForm.type)
    setNotice('Search complete')
  } catch (error) {
    setError(error)
  }
}

function formatDate(value: string | null) {
  if (!value) {
    return ''
  }

  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

function typeLabel(value: string) {
  return value.replace('_', ' ').toUpperCase()
}
</script>

<template>
  <main v-if="isAuthenticated" class="app-shell">
    <header class="topbar">
      <div>
        <strong>Handoff</strong>
        <span>{{ workspace?.name }}</span>
      </div>
      <div class="topbar-actions">
        <span>{{ user?.name }}</span>
        <button class="secondary" type="button" @click="logout">Sign out</button>
      </div>
    </header>

    <div v-if="errorMessage" class="alert error">{{ errorMessage }}</div>
    <div v-if="notice" class="alert notice">{{ notice }}</div>

    <section class="workflow">
      <nav class="steps" aria-label="Workflow">
        <button
          v-for="(stage, index) in stages"
          :key="stage.id"
          :class="{ active: activeStage === stage.id }"
          :disabled="stageDisabled(stage.id)"
          type="button"
          @click="setStage(stage.id)"
        >
          <span>{{ index + 1 }}</span>
          {{ stage.label }}
        </button>
      </nav>

      <div class="selection-bar">
        <span>{{ selectedTicket?.external_id || selectedTicket?.title || 'No ticket selected' }}</span>
        <span>{{ selectedSession?.title || 'No session selected' }}</span>
      </div>

      <section v-if="activeStage === 'tickets'" class="stage-card">
        <div class="stage-header">
          <h1>Tickets</h1>
          <span>{{ tickets.length }} total</span>
        </div>

        <form class="simple-form" @submit.prevent="createTicket">
          <input v-model="ticketForm.external_id" placeholder="TICKET-47" />
          <input v-model="ticketForm.title" placeholder="Ticket title" required />
          <select v-model="ticketForm.source">
            <option v-for="source in ticketSources" :key="source" :value="source">{{ source }}</option>
          </select>
          <button type="submit" :disabled="loading">Create ticket</button>
        </form>

        <div class="item-list">
          <button
            v-for="ticket in tickets"
            :key="ticket.id"
            class="item-row"
            :class="{ active: ticket.id === selectedTicketID }"
            type="button"
            @click="selectTicket(ticket.id)"
          >
            <span>{{ ticket.external_id || ticket.source }}</span>
            <strong>{{ ticket.title }}</strong>
          </button>
        </div>
      </section>

      <section v-if="activeStage === 'sessions'" class="stage-card">
        <div class="stage-header">
          <h1>Sessions</h1>
          <button type="button" :disabled="!selectedTicket" @click="generateHandoff">Handoff</button>
        </div>

        <form class="simple-form" @submit.prevent="createSession">
          <input v-model="sessionForm.title" placeholder="Session title" required />
          <textarea v-model="sessionForm.goal" placeholder="Goal" rows="3" />
          <button type="submit" :disabled="loading || !selectedTicket">Start session</button>
        </form>

        <div class="item-list">
          <button
            v-for="session in sessions"
            :key="session.id"
            class="item-row"
            :class="{ active: session.id === selectedSessionID }"
            type="button"
            @click="selectSession(session.id)"
          >
            <span>{{ session.status }} · {{ formatDate(session.created_at) }}</span>
            <strong>{{ session.title }}</strong>
          </button>
        </div>
      </section>

      <section v-if="activeStage === 'entries'" class="stage-card">
        <div class="stage-header">
          <h1>Entries</h1>
          <button
            class="secondary"
            type="button"
            :disabled="!selectedSession || selectedSession.status === 'completed'"
            @click="completeSession"
          >
            Complete session
          </button>
        </div>

        <form class="entry-form" @submit.prevent="createEntry">
          <select v-model="entryForm.type">
            <option v-for="entryType in entryTypes" :key="entryType" :value="entryType">
              {{ typeLabel(entryType) }}
            </option>
          </select>
          <textarea v-model="entryForm.content" placeholder="Context entry" rows="5" required />
          <button type="submit" :disabled="loading || !selectedSession">Add entry</button>
        </form>

        <div class="entry-list">
          <article v-for="entry in entries" :key="entry.id" class="entry-card">
            <template v-if="editingEntryID === entry.id">
              <select v-model="entryEditForm.type">
                <option v-for="entryType in entryTypes" :key="entryType" :value="entryType">
                  {{ typeLabel(entryType) }}
                </option>
              </select>
              <textarea v-model="entryEditForm.content" rows="5" />
              <div class="row-actions">
                <button type="button" @click="saveEntryEdit">Save</button>
                <button class="secondary" type="button" @click="editingEntryID = ''">Cancel</button>
              </div>
            </template>
            <template v-else>
              <div class="entry-meta">
                <span>{{ typeLabel(entry.type) }}</span>
                <time>{{ formatDate(entry.created_at) }}</time>
              </div>
              <p>{{ entry.content }}</p>
              <div class="row-actions">
                <button class="secondary" type="button" @click="startEditEntry(entry)">Edit</button>
                <button class="danger" type="button" @click="deleteEntry(entry.id)">Delete</button>
              </div>
            </template>
          </article>
        </div>
      </section>

      <section v-if="activeStage === 'handoff'" class="stage-card">
        <div class="stage-header">
          <h1>Handoff</h1>
          <div class="row-actions">
            <button type="button" :disabled="!selectedTicket" @click="generateHandoff">Generate</button>
            <button class="secondary" type="button" :disabled="!handoffContent" @click="copyHandoff">
              Copy
            </button>
          </div>
        </div>
        <pre>{{ handoffContent }}</pre>
      </section>

      <section v-if="activeStage === 'search'" class="stage-card">
        <div class="stage-header">
          <h1>Search</h1>
          <span>{{ searchResults.length }} results</span>
        </div>

        <form class="simple-form" @submit.prevent="runSearch">
          <input v-model="searchForm.q" placeholder="jwt" required />
          <select v-model="searchForm.type">
            <option value="">all types</option>
            <option v-for="entryType in entryTypes" :key="entryType" :value="entryType">
              {{ typeLabel(entryType) }}
            </option>
          </select>
          <button type="submit">Search</button>
        </form>

        <div class="entry-list">
          <article v-for="result in searchResults" :key="result.id" class="entry-card">
            <div class="entry-meta">
              <span>{{ typeLabel(result.type) }}</span>
              <time>{{ formatDate(result.created_at) }}</time>
            </div>
            <strong>{{ result.ticket_external_id || result.ticket_title }}</strong>
            <p>{{ result.content }}</p>
          </article>
        </div>
      </section>
    </section>
  </main>

  <main v-else class="auth-shell">
    <section class="auth-panel">
      <h1>Handoff</h1>
      <div class="segmented">
        <button :class="{ active: authMode === 'login' }" type="button" @click="authMode = 'login'">
          Login
        </button>
        <button
          :class="{ active: authMode === 'register' }"
          type="button"
          @click="authMode = 'register'"
        >
          Register
        </button>
      </div>

      <form v-if="authMode === 'login'" class="stack" @submit.prevent="handleLogin">
        <input v-model="loginForm.email" type="email" placeholder="Email" required />
        <input v-model="loginForm.password" type="password" placeholder="Password" required />
        <button type="submit" :disabled="loading">Login</button>
      </form>

      <form v-else class="stack" @submit.prevent="handleRegister">
        <input v-model="registerForm.name" placeholder="Name" required />
        <input v-model="registerForm.email" type="email" placeholder="Email" required />
        <input v-model="registerForm.password" type="password" placeholder="Password" required />
        <input v-model="registerForm.workspace_name" placeholder="Workspace" required />
        <button type="submit" :disabled="loading">Register</button>
      </form>

      <div v-if="errorMessage" class="alert error">{{ errorMessage }}</div>
      <div v-if="notice" class="alert notice">{{ notice }}</div>
    </section>
  </main>
</template>
