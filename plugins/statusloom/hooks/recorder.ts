import type { EngineInterface, On, Timer } from 'claude-code'

// The command a send runs; it reads one ingest-protocol request on stdin.
const INGEST_ARGV = ['statusloom', 'mod-ingest'] as const
// The ingest-protocol version the request carries as `v`.
const PROTOCOL_VERSION = 1
// A payload value whose JSON is longer than this is left out (named in `_omitted`).
const PAYLOAD_VALUE_MAX = 4096
// At most this many events wait in the outbox; the oldest are dropped beyond.
const OUTBOX_MAX = 500
// At most this many events go in one send.
const BATCH_MAX = 200
// Sends are at least this far apart: the debounce of the send timer.
const DEBOUNCE_MS = 1000
// Queries are taken at most this often after session start.
const QUERY_INTERVAL_MS = 5000
// A send still running after this is given up (its events are kept).
const SEND_TIMEOUT_MS = 10000

type RecordedEvent = { seq: number, at: number, name: string, payload: Record<string, unknown> }
type Query = { at: number, value: unknown }
type Queries = { agents?: Query, usage?: Query }
// The state one registerRecorder call keeps: one call is one boot.
type RecorderState = {
    boot: string,
    seq: number,
    timer: Timer | undefined,
    isInFlight: boolean,
    isQueryDue: boolean,
    lastQueryAt: number | undefined,
    outbox: RecordedEvent[],
}

// The JSON length of `value`: undefined when it has no JSON form, Infinity when it cannot be stringified.
function jsonLength(value: unknown): number | undefined {
    try {
        return JSON.stringify(value)?.length
    } catch {
        return Infinity
    }
}

// A shallow copy of `e`'s top-level keys, leaving out a value whose JSON is too long.
function capPayload(e: object): Record<string, unknown> {
    const payload: Record<string, unknown> = {}
    const omitted: string[] = []
    for (const [key, value] of Object.entries(e)) {
        const length = jsonLength(value)
        if (length === undefined) {
            continue
        }
        if (length > PAYLOAD_VALUE_MAX) {
            omitted.push(key)
            continue
        }
        payload[key] = value
    }
    if (omitted.length > 0) {
        payload._omitted = omitted
    }
    return payload
}

// Arms the send timer when there is something to send and nothing is already queued or in flight.
function schedule(state: RecorderState, $: EngineInterface): void {
    if (state.timer !== undefined || state.isInFlight || (state.outbox.length === 0 && !state.isQueryDue)) {
        return
    }
    try {
        state.timer = $.clock.after(DEBOUNCE_MS, () => {
            state.timer = undefined
            void flush(state, $)
        })
    } catch {
        state.timer = undefined
    }
}

// Appends one event to the outbox and arms the send timer; recording never fails an event.
function record(state: RecorderState, $: EngineInterface, name: string, e: object): void {
    try {
        state.seq += 1
        state.outbox.push({ seq: state.seq, at: Date.now(), name, payload: capPayload(e) })
        if (state.outbox.length > OUTBOX_MAX) {
            state.outbox.splice(0, state.outbox.length - OUTBOX_MAX)
        }
        state.isQueryDue = true
        schedule(state, $)
    } catch {
        // Recording never fails an event.
    }
}

// Reads the agent list and the session usage; a failed read is left out.
async function query($: EngineInterface, now: number): Promise<Queries> {
    const queries: Queries = {}
    try {
        queries.agents = { at: now, value: await $.agent.list() }
    } catch {
        // A failed query is left out.
    }
    try {
        queries.usage = { at: now, value: await $.session.usage({ breakdown: 'summary' }) }
    } catch {
        // A failed query is left out.
    }
    return queries
}

// Sends the queued events (and any due queries) in one ingest-protocol request.
async function flush(state: RecorderState, $: EngineInterface): Promise<void> {
    if (state.isInFlight) {
        return
    }
    state.isInFlight = true
    let isSettled = false
    try {
        const now = await $.clock.now()
        const isQueryTime = state.isQueryDue && (state.lastQueryAt === undefined || now - state.lastQueryAt >= QUERY_INTERVAL_MS)
        let queries: Queries = {}
        if (isQueryTime) {
            state.isQueryDue = false
            state.lastQueryAt = now
            queries = await query($, now)
        }
        const events = state.outbox.slice(0, BATCH_MAX)
        if (events.length === 0 && Object.keys(queries).length === 0) {
            isSettled = true
            return
        }
        const stdin = JSON.stringify({
            v: PROTOCOL_VERSION,
            sessionId: await $.session.id(),
            boot: state.boot,
            events,
            queries,
        })
        const ran = await $.process.run(INGEST_ARGV, { stdin, timeoutMs: SEND_TIMEOUT_MS })
        if (ran.exitCode === 0) {
            const lastSentSeq = events[events.length - 1]?.seq ?? 0
            state.outbox = state.outbox.filter(ev => ev.seq > lastSentSeq)
            isSettled = true
        }
    } catch {
        // A failed send keeps the outbox for the next one.
    } finally {
        state.isInFlight = false
        if (isSettled) {
            schedule(state, $)
        }
    }
}

/**
 * Registers the generic recorder of ingest-protocol (mod obligations) on `on`.
 */
export function registerRecorder(on: On): void {
    const state: RecorderState = {
        boot: crypto.randomUUID(),
        seq: 0,
        timer: undefined,
        isInFlight: false,
        isQueryDue: false,
        lastQueryAt: undefined,
        outbox: [],
    }

    on('session.start', ($, e, next) => {
        state.isQueryDue = true
        schedule(state, $)
        return next(e)
    })
    on('turn.complete', async ($, e, next) => {
        // The session model beside the turn's own model: a model switch has no event of its own.
        let sessionModel: string | undefined
        try {
            sessionModel = await $.session.model()
        } catch {
            // The model is left out.
        }
        record(state, $, 'turn.complete', { ...e, sessionModel, usageModel: e.usage?.model })
        return next(e)
    })
    on('agent.spawn', ($, e, next) => {
        // The parent's permission mode: the only source of the permission-mode field.
        record(state, $, 'agent.spawn', { permissionMode: e.permissionMode, parentAgentId: e.parentAgentId })
        return next(e)
    })
}
