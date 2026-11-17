import { describe, expect, test } from 'claude-code/testing'
import type { On } from 'claude-code'
import { registerAgentRow } from '../hooks/agent-row.js'

type Hook = ($: unknown, e: unknown, next: (e: unknown) => unknown) => Promise<unknown>

type Answer = {
    exitCode: number,
    stdout: string,
    stderr: string,
    isStdoutTruncated: boolean,
    isStderrTruncated: boolean,
}

type Options = {
    agents?: unknown[],
    answer?: Answer,
    run?: () => Promise<never>,
    sessionId?: () => Promise<string>,
    withProcess?: boolean,
}

const OK_ANSWER: Answer = {
    exitCode: 0,
    stdout: '{"v":1,"body":"ROW"}\n',
    stderr: '',
    isStdoutTruncated: false,
    isStderrTruncated: false,
}

/** Registers the hook through a fake `on` and returns a caller with the fake `$`, `next` and run log. */
function harness(options: Options = {}) {
    const regs: unknown[][] = []
    const on = ((...args: unknown[]) => {
        regs.push(args)
    }) as unknown as On
    registerAgentRow(on)
    const hook = regs[0]?.[2] as Hook
    const runs: { argv: readonly string[], init: unknown }[] = []
    const seen: unknown[] = []
    const $: Record<string, unknown> = {
        session: { id: options.sessionId ?? (async () => 'sess-1') },
        agent: { list: async () => options.agents ?? [{ id: 'a' }, { id: 'b', parentId: 'a' }] },
    }
    if (options.withProcess !== false) {
        $.process = {
            run: async (argv: readonly string[], init: unknown) => {
                runs.push({ argv, init })
                if (options.run) return options.run()
                return options.answer ?? OK_ANSWER
            },
        }
    }
    const call = (e: unknown) => hook($, e, (x: unknown) => {
        seen.push(x)
        return 'NEXT'
    })
    return { regs, call, runs, seen }
}

function event(id: string, rowIds: string[], extra: Record<string, unknown> = {}) {
    return Object.freeze({
        surface: 'terminal',
        component: 'AgentRow',
        requestId: id,
        props: Object.freeze({
            id,
            description: 'explore',
            type: 'general-purpose',
            status: 'running',
            kind: 'local_agent',
            taskStatus: 'running',
            label: '',
            startTime: 1700000000000,
            cwd: '/w',
            tokenCount: 10,
            tokenSamples: [],
            hasRow: true,
            concurrent: 1,
            depth: 0,
            isBackgroundEnabled: false,
            rowIds,
            columns: 80,
            isHidden: false,
            ...extra,
        }),
    })
}

describe('registerAgentRow', () => {
    test('registers one ui.render hook, sends the row and merges the body', async () => {
        const h = harness()
        const e = event('a', ['a', 'b'])
        const result = await h.call(e)
        expect(h.regs.length).toBe(1)
        expect(h.runs.length).toBe(1)
        expect(h.runs[0]?.argv).toStrictEqual(['statusloom', 'claude-agent-row'])
        const init = h.runs[0]?.init as { stdin: string }
        expect(h.runs[0]?.init).toStrictEqual({ stdin: init.stdin, timeoutMs: 3000 })
        const request = JSON.parse(init.stdin) as { v: number, sessionId: string, row: unknown, agents: Record<string, unknown>[] }
        expect(request.v).toBe(1)
        expect(request.sessionId).toBe('sess-1')
        expect(request.row).toEqual(e.props)
        expect(request.agents).toEqual([{ id: 'a' }, { id: 'b', parentId: 'a' }])
        expect('parentId' in (request.agents[0] ?? {})).toBe(false)
        expect(h.seen.length).toBe(1)
        expect(result).toBe('NEXT')
        expect(h.seen[0]).toEqual({ ...e, props: { ...e.props, body: 'ROW' } })
    })

    test('an isHidden-only answer sets isHidden and leaves no body key', async () => {
        const h = harness({ answer: { ...OK_ANSWER, stdout: '{"v":1,"isHidden":true}' } })
        await h.call(event('a', ['a']))
        const props = (h.seen[0] as { props: Record<string, unknown> }).props
        expect(props.isHidden).toBe(true)
        expect('body' in props).toBe(false)
    })

    test('an empty string body and false isHidden are kept as answered', async () => {
        const h = harness({ answer: { ...OK_ANSWER, stdout: '{"v":1,"body":"","isHidden":false}' } })
        await h.call(event('a', ['a']))
        const props = (h.seen[0] as { props: Record<string, unknown> }).props
        expect(props.body).toBe('')
        expect(props.isHidden).toBe(false)
    })

    test('every unusable answer hands the untouched event to next', async () => {
        const cases: Answer[] = [
            { ...OK_ANSWER, stdout: '{"v":1}' },
            { ...OK_ANSWER, stdout: '{"v":1,"body":3}' },
            { ...OK_ANSWER, stdout: '{"v":2,"body":"X"}' },
            { ...OK_ANSWER, stdout: '' },
            { ...OK_ANSWER, stdout: 'not json' },
            { ...OK_ANSWER, stdout: '[1]' },
            { ...OK_ANSWER, stdout: 'null' },
            { ...OK_ANSWER, exitCode: 1, stdout: '{"v":1,"body":"X"}' },
        ]
        for (const answer of cases) {
            const h = harness({ answer })
            const e = event('a', ['a'])
            expect(await h.call(e)).toBe('NEXT')
            expect(h.seen.length).toBe(1)
            expect(h.seen[0]).toBe(e)
        }
    })

    test('a rejected run, a missing $.process and a failing session id fall back to next(e)', async () => {
        const rejected = harness({ run: async () => { throw new Error('$.process.run(statusloom) aborted') } })
        const e1 = event('a', ['a'])
        await rejected.call(e1)
        expect(rejected.seen[0]).toBe(e1)

        const noProcess = harness({ withProcess: false })
        const e2 = event('a', ['a'])
        await noProcess.call(e2)
        expect(noProcess.seen[0]).toBe(e2)

        const noSession = harness({ sessionId: async () => { throw new Error('no session') } })
        const e3 = event('a', ['a'])
        await noSession.call(e3)
        expect(noSession.seen[0]).toBe(e3)
        expect(noSession.runs.length).toBe(0)
    })

    test('identical events reuse the cached answer; a changed row spawns again', async () => {
        const h = harness()
        await h.call(event('a', ['a']))
        await h.call(event('a', ['a']))
        expect(h.runs.length).toBe(1)
        expect((h.seen[1] as { props: { body: string } }).props.body).toBe('ROW')

        await h.call(event('a', ['a'], { tokenCount: 11 }))
        expect(h.runs.length).toBe(2)
    })

    test('cache entries for row ids that left rowIds are dropped', async () => {
        const h = harness()
        await h.call(event('a', ['a', 'b']))
        await h.call(event('b', ['b']))
        await h.call(event('a', ['a', 'b']))
        expect(h.runs.length).toBe(3)
    })
})
