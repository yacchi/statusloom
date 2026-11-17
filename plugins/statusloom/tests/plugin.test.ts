import { expect, test } from 'claude-code/testing'
import type { AgentInfo } from 'claude-code'

const AGENT: AgentInfo = {
    id: 'a1',
    description: 'Explore the repo',
    type: 'Explore',
    status: 'running',
    kind: 'local_agent',
    taskStatus: 'running',
    label: 'Explore',
    startTime: 0,
    cwd: '/',
    tokenCount: 0,
    tokenSamples: [],
    hasRow: true,
}

test('the AgentRow relay leaves the engine\'s row when $.process is unavailable', async ($, on) => {
    // Nothing sits beneath the plugin: each hook here stands for the engine.
    // A call on `$` is answered `{ value }`.
    let listCalls = 0
    on('session.id', () => ({ value: 'sess-1' }))
    on('agent.list', () => {
        listCalls++
        return { value: [AGENT] }
    })

    const ui = await $.ui.mount({
        plugin: 'statusloom',
        surface: 'terminal',
        component: 'AgentRow',
        props: { ...AGENT, rowIds: ['a1'], isHidden: false },
    } as never)

    // The relay answered next(e), so the engine's row is what is drawn beneath it.
    expect(await ui.keepsBeneath()).toBe(true)
    expect(listCalls).toBeGreaterThanOrEqual(1)
    await ui.unmount()
})
