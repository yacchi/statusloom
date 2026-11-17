import { describe, expect, test } from 'claude-code/testing'
import type { On } from 'claude-code'
import { registerRecorder } from '../hooks/recorder.js'

type Hook = ($: unknown, e: unknown, next: (e: unknown) => unknown) => unknown
type RunResult = { exitCode: number, stdout: string, stderr: string, isStdoutTruncated: boolean, isStderrTruncated: boolean }
type RunInit = { stdin?: string, timeoutMs?: number }
type Sent = {
    v: number,
    sessionId: string,
    boot: string,
    events: { seq: number, at: number, name: string, payload: Record<string, unknown> }[],
    queries: Record<string, { at: number, value: unknown }>,
}

const T0 = 1_700_000_000_000
const OK: RunResult = { exitCode: 0, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false }
const RECORDED = [
    'turn.complete',
    'agent.spawn',
    'classic.Stop',
    'classic.SubagentStart',
    'classic.SubagentStop',
]

function setup() {
    const regs: unknown[][] = []
    registerRecorder(((...args: unknown[]) => {
        regs.push(args)
    }) as unknown as On)
    const state: { now: number, answer: () => Promise<RunResult>, agents: () => Promise<unknown>, model: () => Promise<string>, clockFails: boolean } = {
        clockFails: false,
        now: T0,
        model: async () => 'claude-opus-4-8',
        answer: async () => OK,
        agents: async () => [{ id: 'a1', status: 'running' }],
    }
    const timers: { ms: number, fn: () => void }[] = []
    const runs: { argv: readonly string[], init: RunInit }[] = []
    const usageArgs: unknown[] = []
    const seen: unknown[] = []
    const $ = {
        clock: {
            now: async () => {
                if (state.clockFails) {
                    throw new Error('no clock')
                }
                return state.now
            },
            after: (ms: number, fn: () => void) => {
                timers.push({ ms, fn })
                return { cancel: () => {} }
            },
        },
        session: {
            id: async () => 'sess-1',
            model: async () => state.model(),
            usage: async (args: unknown) => {
                usageArgs.push(args)
                return { context: { percent: 12 } }
            },
        },
        agent: { list: () => state.agents() },
        process: {
            run: (argv: readonly string[], init: RunInit) => {
                runs.push({ argv, init })
                return state.answer()
            },
        },
    }
    const settle = async () => {
        for (let i = 0; i < 100; i += 1) {
            await Promise.resolve()
        }
    }
    const fire = async () => {
        timers.shift()?.fn()
        await settle()
    }
    const next = (e: unknown) => {
        seen.push(e)
        return 'NEXT'
    }
    const call = (name: string, e: unknown, nextFn: (e: unknown) => unknown = next) => (regs.find(r => r[0] === name)?.[1] as Hook)($, e, nextFn)
    const sent = (i: number) => JSON.parse(runs[i]?.init.stdin ?? 'null') as Sent
    const seqs = (i: number) => sent(i).events.map(ev => ev.seq)
    return { regs, state, timers, runs, usageArgs, seen, call, fire, settle, sent, seqs }
}

describe('registerRecorder', () => {
    test('registers session.start and the recorded events, nothing else', async () => {
        const { regs } = setup()
        expect(regs.map(r => r[0])).toEqual(['session.start', ...RECORDED])
    })

    test('events pass through at once and ride one debounced send', async () => {
        const s = setup()
        const e1 = Object.freeze({ session_id: 's', tool_name: 'Bash' })
        const e2 = Object.freeze({ session_id: 's', stop_hook_active: false })
        const e3 = Object.freeze({ durationMs: 12, reason: 'done' })
        expect(s.call('classic.SubagentStart', e1)).toBe('NEXT')
        expect(s.call('classic.Stop', e2)).toBe('NEXT')
        expect(await s.call('turn.complete', e3)).toBe('NEXT')
        expect(s.seen[0]).toBe(e1)
        expect(s.seen[1]).toBe(e2)
        expect(s.seen[2]).toBe(e3)
        expect(s.timers.map(t => t.ms)).toEqual([1000])
        expect(s.runs.length).toBe(0)

        await s.fire()
        expect(s.runs.length).toBe(1)
        expect(s.runs[0]?.argv).toEqual(['statusloom', 'mod-ingest'])
        expect(s.runs[0]?.init).toStrictEqual({ stdin: expect.any(String), timeoutMs: 10000 })
        const body = s.sent(0)
        expect(body).toMatchObject({ v: 1, sessionId: 'sess-1', boot: expect.any(String) })
        expect(body.events.map(ev => [ev.seq, ev.name])).toEqual([
            [1, 'classic.SubagentStart'],
            [2, 'classic.Stop'],
            [3, 'turn.complete'],
        ])
        expect(body.events[0]?.payload).toEqual(e1)
        expect(body.events[1]?.payload).toEqual(e2)
        expect(body.events[2]?.payload).toEqual({ ...e3, sessionModel: 'claude-opus-4-8' })
        expect(typeof body.events[0]?.at).toBe('number')
        expect(s.timers.length).toBe(0)
    })

    test('a value over 4096 JSON characters is left out and named', async () => {
        const s = setup()
        s.call('classic.SubagentStart', Object.freeze({
            session_id: 's',
            small: 'x'.repeat(4094),
            big: 'x'.repeat(4095),
            tool_response: { lines: Array(1000).fill('line') },
            gone: undefined,
        }))
        const small = Object.freeze({ session_id: 's', tool_name: 'Read', tool_input: { file_path: '/a' } })
        s.call('classic.SubagentStart', small)
        await s.fire()
        expect(s.sent(0).events[0]?.payload).toEqual({
            session_id: 's',
            small: 'x'.repeat(4094),
            _omitted: ['big', 'tool_response'],
        })
        expect(s.sent(0).events[1]?.payload).toEqual(small)
        expect(s.sent(0).events[1]?.payload).not.toHaveProperty('_omitted')
    })

    test('the outbox drops the oldest beyond 500 and sends 200 at a time', async () => {
        const s = setup()
        for (let i = 0; i < 501; i += 1) {
            s.call('classic.SubagentStart', Object.freeze({ i }))
        }
        expect(s.timers.length).toBe(1)
        await s.fire()
        await s.fire()
        await s.fire()
        expect(s.runs.length).toBe(3)
        expect(s.seqs(0)[0]).toBe(2)
        expect(s.seqs(0).at(-1)).toBe(201)
        expect(s.seqs(1)[0]).toBe(202)
        expect(s.seqs(1).at(-1)).toBe(401)
        expect(s.seqs(2)[0]).toBe(402)
        expect(s.seqs(2).at(-1)).toBe(501)
        expect(s.seqs(2).length).toBe(100)
        expect(s.timers.length).toBe(0)
    })

    test('queries at session start, then at most every 5 s', async () => {
        const s = setup()
        expect(s.call('session.start', Object.freeze({ session_id: 's' }))).toBe('NEXT')
        await s.fire()
        expect(s.sent(0).events).toEqual([])
        expect(s.sent(0).queries).toEqual({
            agents: { at: T0, value: [{ id: 'a1', status: 'running' }] },
            usage: { at: T0, value: { context: { percent: 12 } } },
        })
        expect(s.usageArgs).toEqual([{ breakdown: 'summary' }])

        s.state.now = T0 + 4999
        s.call('classic.Stop', Object.freeze({ session_id: 's' }))
        await s.fire()
        expect(s.sent(1).queries).toEqual({})
        expect(s.sent(1).events.length).toBe(1)
        expect(s.timers.length).toBe(1)

        await s.fire()
        expect(s.runs.length).toBe(2)

        s.state.now = T0 + 5000
        await s.fire()
        expect(s.runs.length).toBe(3)
        expect(s.sent(2).events).toEqual([])
        expect(s.sent(2).queries.agents?.at).toBe(T0 + 5000)
        expect(s.timers.length).toBe(0)
    })

    test('a failing query does not stop the events', async () => {
        const s = setup()
        s.state.agents = async () => {
            throw new Error('no agents')
        }
        s.call('classic.Stop', Object.freeze({ session_id: 's' }))
        await s.fire()
        expect(Object.keys(s.sent(0).queries)).toEqual(['usage'])
        expect(s.seqs(0)).toEqual([1])
    })

    test('a failed send keeps its events, arms no retry, and seq never repeats', async () => {
        const s = setup()
        s.state.answer = async () => ({ ...OK, exitCode: 1 })
        s.call('classic.Stop', Object.freeze({ n: 1 }))
        await s.fire()
        expect(s.timers.length).toBe(0)

        s.state.answer = async () => {
            throw new Error('$.process.run(statusloom) timed out')
        }
        s.call('classic.Stop', Object.freeze({ n: 2 }))
        await s.fire()
        expect(s.timers.length).toBe(0)

        s.state.answer = async () => OK
        s.call('classic.Stop', Object.freeze({ n: 3 }))
        await s.fire()
        s.call('classic.Stop', Object.freeze({ n: 4 }))
        await s.fire()
        expect([0, 1, 2, 3].map(i => s.seqs(i))).toEqual([[1], [1, 2], [1, 2, 3], [4]])

        const other = setup()
        other.call('classic.Stop', Object.freeze({ n: 1 }))
        await other.fire()
        expect(other.seqs(0)).toEqual([1])
        expect(other.sent(0).boot).not.toBe(s.sent(0).boot)
    })

    test('a hook never waits on a send in flight', async () => {
        const s = setup()
        s.state.answer = () => new Promise<RunResult>(() => {})
        s.call('classic.Stop', Object.freeze({ n: 1 }))
        await s.fire()
        expect(s.runs.length).toBe(1)
        const e = Object.freeze({ n: 2 })
        expect(s.call('classic.SubagentStart', e)).toBe('NEXT')
        expect(s.seen.at(-1)).toBe(e)
    })

    test('one send in flight at a time', async () => {
        const s = setup()
        let resolve: (r: RunResult) => void = () => {}
        s.state.answer = () => new Promise<RunResult>(r => {
            resolve = r
        })
        s.call('classic.Stop', Object.freeze({ n: 1 }))
        await s.fire()
        expect(s.runs.length).toBe(1)
        s.call('classic.Stop', Object.freeze({ n: 2 }))
        expect(s.timers.length).toBe(0)
        await s.settle()
        expect(s.runs.length).toBe(1)

        s.state.answer = async () => OK
        resolve(OK)
        await s.settle()
        expect(s.timers.length).toBe(1)
        await s.fire()
        expect(s.seqs(1)).toEqual([2])
    })

    test('no timer after a send that emptied the outbox', async () => {
        const s = setup()
        s.call('classic.Stop', Object.freeze({ n: 1 }))
        await s.fire()
        expect(s.runs.length).toBe(1)
        expect(s.timers.length).toBe(0)
    })

    test('turn.complete records the session model beside the usage model, and survives a failing model read', async () => {
        const s = setup()
        const e = Object.freeze({ durationMs: 5, reason: 'answer', usage: { model: 'claude-sonnet-5', input_tokens: 1 } })
        expect(await s.call('turn.complete', e)).toBe('NEXT')
        s.state.model = async () => {
            throw new Error('no model')
        }
        expect(await s.call('turn.complete', e)).toBe('NEXT')
        await s.fire()
        expect(s.sent(0).events[0]?.payload).toMatchObject({ sessionModel: 'claude-opus-4-8', usageModel: 'claude-sonnet-5' })
        expect(s.sent(0).events[1]?.payload).not.toHaveProperty('sessionModel')
        expect(s.sent(0).events[1]?.payload).toMatchObject({ usageModel: 'claude-sonnet-5' })
    })

    test('agent.spawn records the permission mode without the prompt', async () => {
        const s = setup()
        const e = Object.freeze({ prompt: 'secret task', permissionMode: 'plan', parentAgentId: undefined })
        expect(s.call('agent.spawn', e)).toBe('NEXT')
        await s.fire()
        expect(s.sent(0).events[0]).toMatchObject({ name: 'agent.spawn', payload: { permissionMode: 'plan' } })
        expect(s.sent(0).events[0]?.payload).not.toHaveProperty('prompt')
    })
})
