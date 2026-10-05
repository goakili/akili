<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// The session view: persisted history + live timeline for one chat or task session.
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import {
  api,
  ApiError,
  type Approval,
  type Question,
  type BusEvent,
  type Change,
  type ChatSession,
  type DonePayload,
  type Project,
  type SessionEvent,
  type SessionMessage,
  type StatusPayload,
  type Task,
  type ToolOutcome,
  type ToolRequestPayload,
  type ToolResultPayload,
  type UsagePayload,
  IMAGE_TYPES,
  MAX_IMAGE_BYTES,
  MAX_IMAGES_PER_MESSAGE,
} from '../api'
import type { StreamStatus } from '../lib/sse'
import { fmtTime, num, usd } from '../lib/format'
import { fetchAll } from '../lib/paged'
import { phaseLabel, type ChangeChild } from '../lib/tools'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useConfirm } from '../stores/confirm'
import { useLive } from '../stores/live'
import { useToast } from '../stores/toast'
import Badge from './Badge.vue'
import SafeMarkdown from './SafeMarkdown'
import ToolCard from './ToolCard.vue'
import ApprovalCard from './ApprovalCard.vue'
import QuestionCard from './QuestionCard.vue'
import Avatar from './Avatar.vue'
import ProjectChip from './ProjectChip.vue'
import Icon from './Icon'

const props = withDefaults(defineProps<{ sessionId: string; interactive?: boolean }>(), { interactive: true })
const emit = defineEmits<{
  (e: 'session', s: ChatSession): void
  (e: 'task', t: Task): void
  (e: 'notfound'): void
}>()

const auth = useAuth()
const catalog = useCatalog()
const userName = computed(() => auth.user?.name || auth.user?.email || '')
const confirm = useConfirm()
const live = useLive()
const toast = useToast()

const session = ref<ChatSession | null>(null)
/** The project a coding session works on (repo + branch chip in the header). */
const project = ref<Project | null>(null)
const messages = ref<SessionMessage[]>([])
const events = ref<SessionEvent[]>([])
const approvals = ref<Map<string, Approval>>(new Map())
const questions = ref<Map<string, Question>>(new Map())
/** Change plans proposed in this session, by id (live through change.updated). */
const changes = ref<Map<string, Change>>(new Map())
const streaming = ref('')
// Summarised reasoning streamed while the model thinks (display only; cleared when the reply lands).
const reasoning = ref('')
const reasoningTail = computed(() => (reasoning.value.length > 400 ? '…' + reasoning.value.slice(-400) : reasoning.value))
const stateDetail = ref('')
const liveError = ref('')
const loading = ref(true)

interface PendingMsg {
  localId: number
  text: string
  /** Uploaded attachment ids, sent with the text. */
  attachments: string[]
  /** Local previews of those images. */
  previews: string[]
  sentAt: number
  error?: string
  delivered?: boolean
}

/** An image in the composer: uploaded as soon as it is added, sent by id. */
interface DraftImage {
  localId: number
  name: string
  preview: string
  id?: string
  error?: string
}
const images = ref<DraftImage[]>([])
const pending = ref<PendingMsg[]>([])
const draft = ref('')
const sending = ref(false)
let localSeq = 0
let eventSeq = 0

// ---- loading & live ----------------------------------------------------------------------------

async function load() {
  try {
    const d = await api.getSession(props.sessionId, { quiet: true })
    session.value = d.session
    project.value = d.project ?? null
    messages.value = d.messages ?? []
    events.value = d.events ?? []
    approvals.value = new Map((d.approvals ?? []).map((a) => [a.id, a]))
    questions.value = new Map((d.questions ?? []).map((q) => [q.id, q]))
    loadChanges()
    stateDetail.value = ''
    if (d.session.state === 'idle' || d.session.status === 'closed') streaming.value = ''
    // Drop optimistic bubbles the history now contains.
    const sent = messages.value.filter((m) => m.role === 'user').map(sentKey)
    dropPending((p) => !p.error && sent.includes(pendingKey(p)))
    emit('session', d.session)
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) emit('notfound')
    else if (e instanceof ApiError && e.status !== 401) toast.error(e.message)
  } finally {
    loading.value = false
  }
}

async function loadChanges() {
  try {
    const list = await fetchAll((page) => api.pageChanges({ session_id: props.sessionId, page, size: 200 }, { quiet: true }))
    changes.value = new Map(list.map((c) => [c.id, c]))
  } catch {
    /* change cards fall back to the plan alone */
  }
}

function textOf(m: { content: SessionMessage['content'] }): string {
  return (m.content ?? [])
    .filter((b) => b.type === 'text')
    .map((b) => b.text ?? '')
    .join('')
}

/** Matches a stored user message to the optimistic bubble that sent it. */
function sentKey(m: SessionMessage): string {
  const ids = (m.content ?? []).filter((b) => b.type === 'image').map((b) => b.source?.attachment_id ?? '')
  return textOf(m) + '\u0000' + ids.join(',')
}
const pendingKey = (p: PendingMsg) => p.text + '\u0000' + p.attachments.join(',')

function dropPending(match: (p: PendingMsg) => boolean) {
  pending.value = pending.value.filter((p) => {
    if (!match(p)) return true
    p.previews.forEach((u) => URL.revokeObjectURL(u))
    return false
  })
}

function upsertApproval(a: Approval) {
  const m = new Map(approvals.value)
  m.set(a.id, a)
  approvals.value = m
}

function upsertQuestion(q: Question) {
  const m = new Map(questions.value)
  m.set(q.id, q)
  questions.value = m
}

function pushEvent(ev: BusEvent) {
  events.value.push({ id: -++eventSeq, session_id: props.sessionId, type: ev.type, payload: ev.data, created_at: ev.ts })
}

function onEvent(ev: BusEvent) {
  if (ev.session_id && ev.session_id !== props.sessionId) return
  const s = session.value
  switch (ev.type) {
    case 'assistant.delta': {
      const d = ev.data as { text?: string; kind?: string } | undefined
      if (d?.kind === 'thinking') reasoning.value += d.text ?? ''
      else streaming.value += d?.text ?? ''
      break
    }
    case 'agent.status':
      if (ev.agent_id && ev.agent_id === s?.agent_id) {
        catalog.patchAgentStatus(ev.agent_id, (ev.data as { status?: string } | undefined)?.status ?? 'offline')
      }
      break
    case 'message': {
      const m = ev.data as SessionMessage
      if (!m || messages.value.some((x) => x.id === m.id)) break
      messages.value.push(m)
      if (m.role === 'assistant') {
        streaming.value = ''
        reasoning.value = ''
      } else {
        const key = sentKey(m)
        const first = pending.value.find((p) => !p.error && pendingKey(p) === key)
        if (first) dropPending((p) => p === first)
      }
      break
    }
    case 'tool.request':
    case 'tool.result':
    case 'done':
    case 'error':
      pushEvent(ev)
      break
    case 'approval.created':
    case 'approval.resolved':
      if (ev.data) upsertApproval(ev.data as Approval)
      break
    case 'question.created':
    case 'question.resolved':
      if (ev.data) upsertQuestion(ev.data as Question)
      break
    case 'change.updated':
      if (ev.data) {
        const c = ev.data as Change
        const m = new Map(changes.value)
        m.set(c.id, c)
        changes.value = m
      }
      break
    case 'session.state': {
      const st = ev.data as StatusPayload
      if (s) s.state = st.state
      stateDetail.value = st.detail ?? ''
      if (st.state === 'idle') {
        streaming.value = ''
        reasoning.value = ''
      }
      if (st.state !== 'idle') liveError.value = ''
      break
    }
    case 'session.closed':
      if (s) {
        s.status = 'closed'
        s.state = 'idle'
        emit('session', s)
      }
      streaming.value = ''
      break
    case 'session.error':
      liveError.value = (ev.data as { message?: string } | undefined)?.message ?? 'The agent reported an error.'
      break
    case 'usage': {
      const u = ev.data as UsagePayload
      if (s && u) {
        s.input_tokens += u.input_tokens ?? 0
        s.output_tokens += u.output_tokens ?? 0
        s.cost_usd += u.cost_usd ?? 0
      }
      break
    }
    case 'task.updated':
      if (ev.data) emit('task', ev.data as Task)
      break
  }
}

// Live events come from the tab's single shared stream (see stores/live.ts), focused on this session
// so its token deltas are included.
let offEvents: (() => void) | null = null
let offReconnect: (() => void) | null = null
const streamStatus = computed<StreamStatus>(() => live.status)

onMounted(async () => {
  catalog.loadAgents()
  offEvents = live.on(onEvent)
  offReconnect = live.onReconnect(load)
  live.focus(props.sessionId)
  await load()
  scrollToBottom(true)
})

onUnmounted(() => {
  offEvents?.()
  offReconnect?.()
  live.unfocus(props.sessionId)
  images.value.forEach((i) => URL.revokeObjectURL(i.preview))
  pending.value.forEach((p) => p.previews.forEach((u) => URL.revokeObjectURL(u)))
})

// ---- timeline ------------------------------------------------------------------------------------

type Item =
  | { kind: 'text'; key: string; at: number; role: 'user' | 'assistant'; text: string; time: string; images?: string[] }
  | { kind: 'reasoning'; key: string; at: number; text: string }
  | {
      kind: 'tool'
      key: string
      at: number
      name: string
      input: unknown
      request?: ToolRequestPayload
      result?: ToolOutcome
      approval?: Approval
      question?: Question
      change?: Change
      children?: ChangeChild[]
      tag?: string
    }
  | { kind: 'result'; key: string; at: number; content: string; isError: boolean }
  | { kind: 'sys'; key: string; at: number; text: string; tone: '' | 'ok' | 'error' }
  | { kind: 'approval'; key: string; at: number; approval: Approval }
  | { kind: 'question'; key: string; at: number; question: Question }

const ts = (s: string) => new Date(s).getTime() || 0

const timeline = computed<Item[]>(() => {
  const reqByToolUse = new Map<string, ToolRequestPayload>()
  const resultByToolUse = new Map<string, ToolOutcome>()
  const resultBlocks = new Map<string, ToolOutcome>()
  // Calls run inside a change plan have no tool_use_id (the runtime, not the model, makes them).
  const resultByRequest = new Map<string, ToolOutcome>()
  const toolUseIds = new Set<string>()
  for (const e of events.value) {
    if (e.type === 'tool.request') {
      const p = e.payload as ToolRequestPayload
      if (p?.tool_use_id) reqByToolUse.set(p.tool_use_id, p)
    } else if (e.type === 'tool.result') {
      const p = e.payload as ToolResultPayload
      if (p?.tool_use_id) resultByToolUse.set(p.tool_use_id, p)
      if (p?.request_id) resultByRequest.set(p.request_id, p)
    }
  }
  const changeByApproval = new Map<string, Change>()
  for (const c of changes.value.values()) if (c.approval_id) changeByApproval.set(c.approval_id, c)
  const changeFor = (name: string, req?: ToolRequestPayload, ap?: Approval) =>
    name === 'change_run' ? changeByApproval.get(req?.approval_id || ap?.id || '') : undefined
  // Calls that belong to a change: numbered per phase, grouped under the change_run card.
  const childrenByChange = new Map<string, (ChangeChild & { at: number })[]>()
  const phaseCount = new Map<string, number>()
  for (const m of messages.value) {
    for (const b of m.content ?? []) {
      if (b.type === 'tool_use' && b.id) toolUseIds.add(b.id)
      if (b.type === 'tool_result' && b.tool_use_id) resultBlocks.set(b.tool_use_id, { output: b.content ?? '', is_error: !!b.is_error })
    }
  }
  const approvalByRequest = new Map<string, Approval>()
  for (const a of approvals.value.values()) approvalByRequest.set(a.request_id, a)
  const claimedApprovals = new Set<string>()
  const findApproval = (req?: ToolRequestPayload) => {
    if (!req) return undefined
    const a = (req.approval_id && approvals.value.get(req.approval_id)) || approvalByRequest.get(req.request_id)
    if (a) claimedApprovals.add(a.id)
    return a
  }

  const questionByRequest = new Map<string, Question>()
  for (const q of questions.value.values()) questionByRequest.set(q.request_id, q)
  const claimedQuestions = new Set<string>()
  const findQuestion = (req?: ToolRequestPayload) => {
    const q = req ? questionByRequest.get(req.request_id) : undefined
    if (q) claimedQuestions.add(q.id)
    return q
  }

  const items: Item[] = []
  for (const m of messages.value) {
    const at = ts(m.created_at)
    let reasoningItem: { kind: 'reasoning'; key: string; at: number; text: string } | null = null
    // A message's images show in the bubble of its text (or alone when there is none).
    const imageUrls = (m.content ?? [])
      .filter((b) => b.type === 'image' && b.source?.attachment_id)
      .map((b) => api.attachmentUrl(m.session_id, b.source!.attachment_id))
    let imagesPlaced = false
    ;(m.content ?? []).forEach((b, i) => {
      const key = `m${m.id}-${i}`
      switch (b.type) {
        case 'text':
          if (b.text?.trim()) {
            items.push({ kind: 'text', key, at, role: m.role, text: b.text, time: fmtTime(m.created_at), images: imagesPlaced ? undefined : imageUrls })
            imagesPlaced = true
          }
          break
        case 'image':
          if (!imagesPlaced && !(m.content ?? []).some((x) => x.type === 'text' && x.text?.trim())) {
            items.push({ kind: 'text', key, at, role: m.role, text: '', time: fmtTime(m.created_at), images: imageUrls })
            imagesPlaced = true
          }
          break
        case 'thinking':
        case 'redacted_thinking': {
          const t = b.type === 'thinking' ? (b.thinking ?? '') : ''
          if (!reasoningItem) {
            reasoningItem = { kind: 'reasoning', key, at, text: t }
            items.push(reasoningItem)
          } else if (t) reasoningItem.text += (reasoningItem.text ? '\n\n' : '') + t
          break
        }
        case 'tool_use': {
          const req = b.id ? reqByToolUse.get(b.id) : undefined
          const name = b.name ?? req?.tool ?? 'tool'
          const approval = findApproval(req)
          items.push({
            kind: 'tool',
            key,
            at,
            name,
            input: b.input,
            request: req,
            result: b.id ? (resultByToolUse.get(b.id) ?? resultBlocks.get(b.id)) : undefined,
            approval,
            question: findQuestion(req),
            change: changeFor(name, req, approval),
          })
          break
        }
        case 'tool_result':
          if (!b.tool_use_id || !toolUseIds.has(b.tool_use_id)) {
            items.push({ kind: 'result', key, at, content: b.content ?? '', isError: !!b.is_error })
          }
          break
      }
    })
  }

  for (const e of events.value) {
    const at = ts(e.created_at)
    const key = `e${e.id}`
    if (e.type === 'tool.request') {
      const p = e.payload as ToolRequestPayload
      if (p?.tool_use_id && toolUseIds.has(p.tool_use_id)) continue
      const result = (p.tool_use_id && resultByToolUse.get(p.tool_use_id)) || resultByRequest.get(p.request_id)
      if (p.change_id) {
        const ck = `${p.change_id}:${p.phase}`
        const n = (phaseCount.get(ck) ?? 0) + 1
        phaseCount.set(ck, n)
        const list = childrenByChange.get(p.change_id) ?? []
        list.push({ key, at, name: p.tool, input: p.input, request: p, result, tag: `change · ${phaseLabel(p.phase)} ${n}` })
        childrenByChange.set(p.change_id, list)
        continue
      }
      const approval = findApproval(p)
      items.push({ kind: 'tool', key, at, name: p.tool, input: p.input, request: p, result, approval, question: findQuestion(p), change: changeFor(p.tool, p, approval) })
    } else if (e.type === 'done') {
      const d = e.payload as DonePayload
      const ok = d?.outcome === 'succeeded'
      const text = `Task ${String(d?.outcome ?? 'finished').replace('_', ' ')}${d?.summary ? ': ' + d.summary : ''}${d?.error ? ' — ' + d.error : ''}`
      items.push({ kind: 'sys', key, at, text, tone: ok ? 'ok' : 'error' })
    } else if (e.type === 'error') {
      items.push({ kind: 'sys', key, at, text: (e.payload as { message?: string })?.message ?? 'Error', tone: 'error' })
    }
  }
  // Group change calls under their change_run card; calls whose card is not shown stand alone, tagged.
  for (const it of items) {
    if (it.kind === 'tool' && it.change) {
      const kids = childrenByChange.get(it.change.id)
      if (kids) {
        it.children = kids
        childrenByChange.delete(it.change.id)
      }
    }
  }
  const shownRequests = new Set<string>()
  for (const kids of childrenByChange.values()) {
    for (const k of kids) items.push({ kind: 'tool', key: k.key, at: k.at, name: k.name, input: k.input, request: k.request, result: k.result, tag: k.tag })
  }
  for (const e of events.value) {
    if (e.type === 'tool.request') shownRequests.add((e.payload as ToolRequestPayload)?.request_id)
  }
  // tool.result events whose request we never saw (e.g. history trimmed)
  const shown = new Set(items.filter((i) => i.kind === 'tool').map((i) => (i as { request?: ToolRequestPayload }).request?.tool_use_id).filter(Boolean))
  for (const e of events.value) {
    if (e.type !== 'tool.result') continue
    const p = e.payload as ToolResultPayload
    if (!p || (p.tool_use_id && (shown.has(p.tool_use_id) || toolUseIds.has(p.tool_use_id))) || shownRequests.has(p.request_id)) continue
    items.push({ kind: 'tool', key: `e${e.id}`, at: ts(e.created_at), name: p.tool, input: null, result: p, tag: p.change_id ? `change · ${phaseLabel(p.phase)}` : undefined })
  }
  for (const a of approvals.value.values()) {
    if (!claimedApprovals.has(a.id)) items.push({ kind: 'approval', key: `a${a.id}`, at: ts(a.created_at), approval: a })
  }
  for (const q of questions.value.values()) {
    if (!claimedQuestions.has(q.id)) items.push({ kind: 'question', key: `q${q.id}`, at: ts(q.created_at), question: q })
  }
  return items
    .map((it, idx) => ({ it, idx }))
    .sort((x, y) => x.it.at - y.it.at || x.idx - y.idx)
    .map((x) => x.it)
})

// ---- scrolling ---------------------------------------------------------------------------------

const scroller = ref<HTMLElement | null>(null)
const stick = ref(true)
function onScroll() {
  const el = scroller.value
  if (!el) return
  stick.value = el.scrollHeight - el.scrollTop - el.clientHeight < 80
}
async function scrollToBottom(force = false) {
  await nextTick()
  const el = scroller.value
  if (el && (force || stick.value)) el.scrollTo({ top: el.scrollHeight, behavior: force ? 'auto' : 'smooth' })
}
function jumpToLatest() {
  stick.value = true
  const el = scroller.value
  if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
}
const thinkingOpen = ref(false)
watch(() => [timeline.value.length, streaming.value.length, pending.value.length], () => scrollToBottom())

// ---- actions -----------------------------------------------------------------------------------

const agent = computed(() => catalog.agents.find((a) => a.id === session.value?.agent_id))
const isOpen = computed(() => session.value?.status === 'open')
const canWrite = computed(() => props.interactive && auth.isOperator && isOpen.value)
const busyState = computed(() => !!session.value?.state && session.value.state !== 'idle')
const agentOffline = computed(() => !!agent.value && agent.value.status !== 'online')
const showComposer = computed(() => props.interactive && auth.isOperator && !!session.value)
const composerBlocked = computed(() => {
  if (!session.value) return ''
  if (!isOpen.value) return 'This session is closed. Start a new chat to continue.'
  if (agentOffline.value) return `${agent.value?.name ?? 'The agent'} is ${agent.value?.status}; messages can't be delivered until it reconnects.`
  return ''
})

const uploading = computed(() => images.value.some((i) => !i.id && !i.error))
const readyImages = computed(() => images.value.filter((i) => i.id))
const canSend = computed(
  () => (!!draft.value.trim() || readyImages.value.length > 0) && !uploading.value && !sending.value && !composerBlocked.value,
)

function addImages(files: Iterable<File>) {
  for (const f of files) {
    if (!IMAGE_TYPES.includes(f.type)) {
      toast.error(`${f.name || 'This file'} is not a PNG, JPEG, GIF or WebP image.`)
      continue
    }
    if (f.size > MAX_IMAGE_BYTES) {
      toast.error(`${f.name || 'The image'} is larger than 5 MB.`)
      continue
    }
    if (images.value.length >= MAX_IMAGES_PER_MESSAGE) {
      toast.error(`At most ${MAX_IMAGES_PER_MESSAGE} images per message.`)
      break
    }
    const img: DraftImage = { localId: ++localSeq, name: f.name || 'pasted image', preview: URL.createObjectURL(f) }
    images.value.push(img)
    api
      .uploadAttachment(props.sessionId, f, { quiet: true })
      .then((a) => {
        const it = images.value.find((x) => x.localId === img.localId)
        if (it) it.id = a.id
      })
      .catch((e) => {
        const it = images.value.find((x) => x.localId === img.localId)
        if (it) it.error = e instanceof ApiError ? e.message : 'Upload failed.'
      })
  }
}

function removeImage(img: DraftImage) {
  URL.revokeObjectURL(img.preview)
  images.value = images.value.filter((x) => x.localId !== img.localId)
}

const fileInput = ref<HTMLInputElement | null>(null)
function onFiles(e: Event) {
  const input = e.target as HTMLInputElement
  if (input.files) addImages(input.files)
  input.value = ''
}
function onPaste(e: ClipboardEvent) {
  const files = [...(e.clipboardData?.files ?? [])].filter((f) => f.type.startsWith('image/'))
  if (files.length) {
    e.preventDefault()
    addImages(files)
  }
}
const dragging = ref(false)
function onDrop(e: DragEvent) {
  dragging.value = false
  if (e.dataTransfer?.files.length && canWrite.value && !composerBlocked.value) addImages(e.dataTransfer.files)
}

async function send(text?: string, retryOf?: PendingMsg) {
  const body = (text ?? draft.value).trim()
  if (!canWrite.value || composerBlocked.value) return
  let attachments: string[]
  let previews: string[]
  if (retryOf) {
    attachments = retryOf.attachments
    previews = retryOf.previews
  } else {
    if (uploading.value) return
    attachments = readyImages.value.map((i) => i.id!)
    previews = readyImages.value.map((i) => i.preview)
  }
  if (!body && !attachments.length) return
  const p: PendingMsg = { localId: ++localSeq, text: body, attachments, previews, sentAt: Date.now() }
  if (text === undefined) {
    draft.value = ''
    images.value.filter((i) => !i.id).forEach((i) => URL.revokeObjectURL(i.preview))
    images.value = []
  }
  pending.value.push(p)
  stick.value = true
  sending.value = true
  try {
    await api.postMessage(props.sessionId, body, attachments, { quiet: true })
    const sent = pending.value.find((x) => x.localId === p.localId)
    if (sent) sent.delivered = true
    liveError.value = ''
  } catch (e) {
    const target = pending.value.find((x) => x.localId === p.localId)
    const msg =
      e instanceof ApiError && e.status === 409
        ? /not connected/i.test(e.message)
          ? 'The agent is not connected, so the message was not delivered.'
          : e.message
        : e instanceof ApiError
          ? e.message
          : 'Message not delivered.'
    if (target) target.error = msg
  } finally {
    sending.value = false
  }
}

function retry(p: PendingMsg) {
  pending.value = pending.value.filter((x) => x.localId !== p.localId)
  send(p.text, p)
}

function discard(p: PendingMsg) {
  dropPending((x) => x.localId === p.localId)
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    send()
  }
}

const composer = ref<HTMLTextAreaElement | null>(null)
watch(draft, async () => {
  await nextTick()
  const el = composer.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 220) + 'px'
})

async function interrupt() {
  try {
    await api.interruptSession(props.sessionId)
    toast.info('Interrupt sent')
  } catch {
    /* toasted */
  }
}

async function close() {
  const ok = await confirm.ask({
    title: 'Close this session?',
    message: session.value?.mode === 'task' ? 'Closing a task session stops the agent working on it.' : 'The conversation ends; history stays available.',
    confirmText: 'Close session',
    danger: true,
  })
  if (!ok) return
  try {
    await api.closeSession(props.sessionId)
    if (session.value) session.value.status = 'closed'
  } catch {
    /* toasted */
  }
}

const stateLabel = computed(() => {
  switch (session.value?.state) {
    case 'thinking':
      return 'Thinking'
    case 'running_tool':
      return 'Running tool'
    case 'waiting_approval':
      return 'Waiting for approval'
    case 'waiting_input':
      return 'Waiting for your answer'
    default:
      return 'Idle'
  }
})

defineExpose({ reload: load })
</script>

<template>
  <div class="card chat">
    <header class="chat-head">
      <div class="ch-agent grow">
        <Avatar agent />
        <div style="min-width: 0">
          <div class="ch-name">
            <RouterLink v-if="session" :to="`/agents/${session.agent_id}`" style="color: inherit">{{ catalog.agentName(session?.agent_id) }}</RouterLink>
            <span class="status-dot" :class="agent?.status" :title="agent ? `Agent ${agent.status}` : ''" aria-hidden="true" />
            <span class="sr-only">{{ agent ? `agent ${agent.status}` : '' }}</span>
          </div>
          <div class="ch-meta">
            <Badge
              v-if="session"
              :value="session.status === 'closed' ? 'closed' : session.state || 'idle'"
              :label="session.status === 'closed' ? 'closed' : stateLabel"
            />
            <ProjectChip v-if="session?.project_id" :project="project" :branch="session.branch" link />
            <span v-if="stateDetail && session?.state !== 'idle'" class="truncate" style="max-width: 260px">{{ stateDetail }}</span>
            <span v-if="streamStatus !== 'open'" class="row" role="status" style="gap: 5px">
              <span class="live-dot" :class="streamStatus" />{{ streamStatus === 'closed' ? 'live updates off' : 'reconnecting…' }}
            </span>
          </div>
        </div>
      </div>
      <div class="row wrap">
        <span v-if="session" class="small muted num" :title="`${num(session.input_tokens)} input · ${num(session.output_tokens)} output tokens`">
          <Icon name="zap" :size="13" style="vertical-align: -2px" /> {{ num(session.input_tokens + session.output_tokens) }} tok · {{ usd(session.cost_usd) }}
        </span>
        <template v-if="interactive && auth.isOperator && isOpen">
          <button type="button" class="btn btn-sm" :disabled="!busyState" title="Stop the agent's current turn" @click="interrupt">
            <Icon name="stop" />Interrupt
          </button>
          <button type="button" class="btn btn-sm btn-danger-ghost" @click="close"><Icon name="x" />Close</button>
        </template>
      </div>
    </header>

    <div class="chat-body">
      <div ref="scroller" class="chat-scroll" @scroll="onScroll">
        <div class="thread" role="log" aria-live="polite" aria-label="Conversation">
          <template v-if="loading">
            <div v-for="i in 3" :key="i" class="msg" :class="i % 2 ? 'assistant' : 'user'" aria-hidden="true">
              <span class="skel circle" style="width: 32px; height: 32px; flex: none" />
              <div class="bubble" style="width: 280px"><span class="skel" style="width: 90%" /><span class="skel" style="width: 60%; margin-top: 8px" /></div>
            </div>
          </template>
          <div v-else-if="!timeline.length && !streaming && !pending.length" class="empty">
            <div class="empty-ic"><Icon name="chat" /></div>
            <strong>No messages yet</strong>
            <p v-if="canWrite">Say hello. The agent answers here, and every tool call it makes shows up inline for review.</p>
          </div>

          <template v-for="it in timeline" :key="it.key">
            <div v-if="it.kind === 'text'" class="msg" :class="it.role">
              <Avatar v-if="it.role === 'user'" :name="userName" small />
              <Avatar v-else agent small />
              <div class="msg-col">
                <div v-if="it.images?.length" class="msg-images">
                  <a v-for="(src, i) in it.images" :key="src" :href="src" target="_blank" rel="noopener">
                    <img :src="src" :alt="`Image ${i + 1} attached to the message`" loading="lazy" />
                  </a>
                </div>
                <div v-if="it.text" class="bubble"><SafeMarkdown :text="it.text" /></div>
                <span class="meta">{{ it.role === 'user' ? 'You' : catalog.agentName(session?.agent_id) }} · {{ it.time }}</span>
              </div>
            </div>
            <details v-else-if="it.kind === 'reasoning'" class="thinking thread-item">
              <summary><Icon name="brain" />Thought process<Icon name="chevronDown" class="chev" /></summary>
              <div class="think-text">{{ it.text || 'The model reasoned before answering (content not retained).' }}</div>
            </details>
            <div v-else-if="it.kind === 'tool' && it.question" class="thread-item">
              <QuestionCard :question="it.question" @resolved="upsertQuestion" />
            </div>
            <div v-else-if="it.kind === 'tool'" class="thread-item">
              <ToolCard
                :name="it.name"
                :input="it.input"
                :request="it.request"
                :result="it.result"
                :approval="it.approval"
                :change="it.change"
                :children="it.children"
                :tag="it.tag"
                @approval="upsertApproval"
              />
            </div>
            <details v-else-if="it.kind === 'result'" class="tool-card thread-item" :class="{ denied: it.isError }">
              <summary>
                <Icon name="chevronRight" class="chev" />
                <span class="tc-icon" aria-hidden="true"><Icon name="wrench" /></span>
                <span class="tc-name">tool result</span>
                <span class="grow" />
                <Badge :value="it.isError ? 'error' : 'ok'" />
              </summary>
              <div class="tc-body"><pre class="code" :class="{ error: it.isError }">{{ it.content || '(no output)' }}</pre></div>
            </details>
            <div v-else-if="it.kind === 'sys'" class="sys-row" :class="it.tone">
              <Icon :name="it.tone === 'ok' ? 'checkCircle' : it.tone === 'error' ? 'xCircle' : 'info'" />{{ it.text }}
            </div>
            <div v-else-if="it.kind === 'approval'" class="thread-item">
              <ApprovalCard :approval="it.approval" @resolved="upsertApproval" />
            </div>
            <div v-else-if="it.kind === 'question'" class="thread-item">
              <QuestionCard :question="it.question" @resolved="upsertQuestion" />
            </div>
          </template>

          <div v-for="p in pending" :key="`p${p.localId}`" class="msg user">
            <Avatar :name="userName" small />
            <div class="msg-col">
              <div v-if="p.previews.length" class="msg-images" :class="{ pending: !p.error }">
                <img v-for="(src, i) in p.previews" :key="src" :src="src" :alt="`Image ${i + 1} being sent`" />
              </div>
              <div v-if="p.text" class="bubble" :class="{ pending: !p.error, failed: p.error }"><SafeMarkdown :text="p.text" /></div>
              <span v-if="p.error" class="meta danger-text" role="alert">
                {{ p.error }}
                <button type="button" class="btn-link" @click="retry(p)">Retry</button> ·
                <button type="button" class="btn-link" @click="discard(p)">Discard</button>
              </span>
              <span v-else class="meta">{{ p.delivered ? 'Delivered · waiting for the agent' : 'Sending…' }}</span>
            </div>
          </div>

          <details
            v-if="(session?.state === 'thinking' || reasoning) && isOpen && !streaming"
            class="thinking live thread-item"
            :open="thinkingOpen"
            role="status"
            @toggle="thinkingOpen = ($event.target as HTMLDetailsElement).open"
          >
            <summary>
              <Icon name="brain" class="brain" />Thinking<span class="typing-dots" aria-hidden="true"><span /><span /><span /></span>
              <Icon v-if="reasoning" name="chevronDown" class="chev" />
            </summary>
            <div v-if="reasoning" class="think-text">{{ reasoningTail }}</div>
          </details>
          <div v-if="streaming" class="msg assistant">
            <Avatar agent small />
            <div class="msg-col">
              <div class="bubble typing"><SafeMarkdown :text="streaming" /></div>
            </div>
          </div>
        </div>
      </div>
      <button v-if="!stick && timeline.length" type="button" class="btn btn-sm jump-latest" @click="jumpToLatest">
        <Icon name="arrowDown" />Jump to latest
      </button>
    </div>

    <div v-if="liveError" class="banner danger" style="margin: 0 16px 10px; border-radius: var(--radius)" role="alert">
      <Icon name="alert" />
      <span class="banner-body">{{ liveError }}</span>
      <button type="button" class="btn btn-ghost btn-xs btn-icon" aria-label="Dismiss error" @click="liveError = ''"><Icon name="x" /></button>
    </div>

    <div
      v-if="showComposer && isOpen"
      class="composer-wrap"
      :class="{ dragging }"
      @dragover.prevent="dragging = !composerBlocked"
      @dragleave.self="dragging = false"
      @drop.prevent="onDrop"
    >
      <div v-if="composerBlocked" class="composer-note" role="status"><Icon name="alert" />{{ composerBlocked }}</div>
      <ul v-if="images.length" class="composer-images" aria-label="Images to send">
        <li v-for="img in images" :key="img.localId" :class="{ failed: img.error }" :title="img.error || img.name">
          <img :src="img.preview" :alt="img.name" />
          <span v-if="!img.id && !img.error" class="ci-state"><span class="spinner" /></span>
          <span v-else-if="img.error" class="ci-state" role="alert"><Icon name="alert" /></span>
          <button type="button" class="ci-remove" :aria-label="`Remove ${img.name}`" @click="removeImage(img)"><Icon name="x" /></button>
        </li>
      </ul>
      <form class="composer" :class="{ disabled: !!composerBlocked }" @submit.prevent="send()">
        <input ref="fileInput" type="file" :accept="IMAGE_TYPES.join(',')" multiple hidden @change="onFiles" />
        <button
          type="button"
          class="btn btn-ghost btn-icon composer-attach"
          :disabled="!!composerBlocked || images.length >= MAX_IMAGES_PER_MESSAGE"
          aria-label="Attach images"
          title="Attach images (PNG, JPEG, GIF or WebP, up to 5 MB)"
          @click="fileInput?.click()"
        >
          <Icon name="image" />
        </button>
        <label for="composer" class="sr-only">Message</label>
        <textarea
          id="composer"
          ref="composer"
          v-model="draft"
          rows="1"
          :disabled="!!composerBlocked"
          :placeholder="composerBlocked ? 'Messaging is unavailable' : `Message ${catalog.agentName(session?.agent_id)}…`"
          aria-describedby="composer-hint"
          @keydown="onKey"
          @paste="onPaste"
        />
        <button type="submit" class="btn btn-primary btn-icon" :disabled="!canSend" aria-label="Send message">
          <span v-if="sending" class="spinner" /><Icon v-else name="send" />
        </button>
      </form>
      <div id="composer-hint" class="composer-hint">
        <span><kbd>Enter</kbd> to send · <kbd>Shift</kbd>+<kbd>Enter</kbd> for a new line · paste or drop images</span>
        <span class="hide-mobile">Tool calls are checked against the agent's policy</span>
      </div>
    </div>
    <div v-else-if="session && interactive" class="composer-closed">
      <Icon name="lock" />{{ !isOpen ? 'This session is closed. History stays available.' : 'Viewers can read sessions but not send messages.' }}
    </div>
  </div>
</template>
