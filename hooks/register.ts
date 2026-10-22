import type { Register, EngineInterface } from 'claude-code'

// What the light does in each state, as a `lux mood` command line. lux mood
// stops the previous effect, starts the next one detached, and exits 1 at once
// when no Flag is plugged in, so without the device nothing happens.
const MOOD = {
  idle: ['matrix'], // green rain: waiting for you
  thinking: ['pulse', 'claude'], // breathing Claude orange
  editing: ['chase', 'claude'], // Claude orange racing round the LEDs: writing code
  running: ['wave', 'cyan'], // cyan wave: a command or tool is running
  waiting: ['police'], // red and blue: Claude needs your answer
  off: ['off'],
} as const
type Mood = keyof typeof MOOD

const EDIT_TOOLS = new Set(['Edit', 'Write', 'MultiEdit', 'NotebookEdit'])

// A state stays on the light at least this long, so a tool call that finishes
// in 50ms still reads as cyan instead of a flicker. Off is never delayed.
const DWELL_MS = 1500

// Bursts of state changes coalesce: only the latest wanted mood is applied,
// one lux mood at a time, so two never race for the light.
let wanted: Mood = 'off'
let applied: Mood | undefined
let appliedAt = 0
let driving: Promise<void> = Promise.resolve()

function set($: EngineInterface, mood: Mood): Promise<void> {
  wanted = mood
  if (wanted !== applied) {
    driving = driving.then(async () => {
      while (wanted !== applied) {
        if (wanted !== 'off') {
          const wait = DWELL_MS - (Date.now() - appliedAt)
          if (wait > 0) await $.clock.sleep(wait)
        }
        const next = wanted
        if (next === applied) break
        await $.process.run([`${$.plugin.root}/bin/lux`, 'mood', ...MOOD[next]]).catch(() => undefined)
        applied = next
        appliedAt = Date.now()
      }
    })
  }
  return driving
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    void set($, 'idle')
    return started
  })

  on('turn.start', ($, e, next) => {
    if (e.agentId === undefined) void set($, 'thinking')
    return next(e)
  })

  on('tool.call', async ($, e, next) => {
    if (e.agentId !== undefined) return next(e)
    void set($, EDIT_TOOLS.has(e.tool) ? 'editing' : 'running')
    try {
      return await next(e)
    } finally {
      void set($, 'thinking')
    }
  })

  on('classic.Notification', { notification_type: 'permission_prompt' }, ($, e, next) => {
    void set($, 'waiting')
    return next(e)
  })

  on('turn.complete', ($, e, next) => {
    if (e.agentId === undefined) void set($, 'idle')
    return next(e)
  })

  on('session.end', async ($, e, next) => {
    await set($, 'off')
    return next(e)
  })
}
