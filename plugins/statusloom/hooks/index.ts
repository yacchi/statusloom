import type { Register } from 'claude-code'
import { registerAgentRow } from './agent-row.js'
import { registerRecorder } from './recorder.js'

/**
 * The statusloom mod's one hooks module: the AgentRow relay and the generic recorder.
 */
export const register: Register = on => {
    registerAgentRow(on)
    registerRecorder(on)
}
