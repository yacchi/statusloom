// Bounded undo/redo history. Pure functions over an immutable present value so
// they are trivially testable and framework-agnostic.

export const HISTORY_LIMIT = 100;

export interface History<T> {
    past: T[];
    present: T;
    future: T[];
}

export function initHistory<T>(present: T): History<T> {
    return { past: [], present, future: [] };
}

// Record a new present value, clearing the redo stack. The `past` stack is
// capped at `limit` entries (oldest dropped first).
export function pushHistory<T>(history: History<T>, next: T, limit: number = HISTORY_LIMIT): History<T> {
    const past = [...history.past, history.present];
    while (past.length > limit) {
        past.shift();
    }
    return { past, present: next, future: [] };
}

export function canUndo<T>(history: History<T>): boolean {
    return history.past.length > 0;
}

export function canRedo<T>(history: History<T>): boolean {
    return history.future.length > 0;
}

export function undo<T>(history: History<T>): History<T> {
    if (history.past.length === 0) {
        return history;
    }
    const previous = history.past[history.past.length - 1];
    return {
        past: history.past.slice(0, -1),
        present: previous,
        future: [history.present, ...history.future],
    };
}

export function redo<T>(history: History<T>): History<T> {
    if (history.future.length === 0) {
        return history;
    }
    const next = history.future[0];
    return {
        past: [...history.past, history.present],
        present: next,
        future: history.future.slice(1),
    };
}
