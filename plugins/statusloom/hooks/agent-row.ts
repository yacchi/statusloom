import type { On } from 'claude-code'

/** Argument vector of the statusloom binary's AgentRow subcommand. */
const AGENT_ROW_ARGV = ['statusloom', 'claude-agent-row'] as const

/** Upper bound for one `statusloom claude-agent-row` run, in milliseconds. */
const TIMEOUT_MS = 3000

/** Protocol version of the agent-row-protocol, in both directions. */
const PROTOCOL_VERSION = 1

/** What the binary asks the host to change on an AgentRow; only the keys it sent. */
type Patch = { body?: string, isHidden?: boolean }

/**
 * Reads the binary's answer. Returns undefined for any answer that must fall back to the
 * unmodified event; otherwise the patch, possibly empty (a valid "leave the row" answer).
 */
function parseAnswer(ran: { exitCode: number, stdout: string }): Patch | undefined {
    if (ran.exitCode !== 0) return undefined
    let obj: unknown
    try {
        obj = JSON.parse(ran.stdout.trim())
    } catch {
        return undefined
    }
    if (typeof obj !== 'object' || obj === null || Array.isArray(obj)) return undefined
    const answer = obj as { v?: unknown, body?: unknown, isHidden?: unknown }
    if (answer.v !== PROTOCOL_VERSION) return undefined
    const patch: Patch = {}
    if (typeof answer.body === 'string') patch.body = answer.body
    if (typeof answer.isHidden === 'boolean') patch.isHidden = answer.isHidden
    return patch
}

/**
 * Registers the AgentRow relay of the agent-row-protocol (mod side) on `on`.
 * The answer cache lives in the closure of this call, not at module level.
 */
export function registerAgentRow(on: On): void {
    const cache = new Map<string, { request: string, patch: Patch }>()
    on('ui.render', { component: 'AgentRow' }, async ($, e, next) => {
        for (const id of cache.keys()) {
            if (!e.props.rowIds.includes(id)) cache.delete(id)
        }
        const id = e.props.id
        let patch: Patch | undefined = undefined
        try {
            const sessionId = await $.session.id()
            const agents = (await $.agent.list()).map(a => a.parentId === undefined ? { id: a.id } : { id: a.id, parentId: a.parentId })
            const request = JSON.stringify({ v: PROTOCOL_VERSION, sessionId, row: e.props, agents })
            const cached = cache.get(id)
            if (cached?.request === request) {
                patch = cached.patch
            } else {
                const ran = await $.process.run(AGENT_ROW_ARGV, { stdin: request, timeoutMs: TIMEOUT_MS })
                patch = parseAnswer(ran)
                if (patch !== undefined) cache.set(id, { request, patch })
            }
        } catch {
            patch = undefined
        }
        if (patch === undefined || (patch.body === undefined && patch.isHidden === undefined)) return next(e)
        return next({ ...e, props: { ...e.props, ...patch } })
    })
}
