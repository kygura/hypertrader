import { useEffect, useRef } from 'react'
import { coreWS, type CoreFrame } from '../lib/core-client'

export type CoreStreamTopic = 'bar' | 'verdict' | 'journal' | 'status' | 'mids'
type Handler = (data: unknown) => void

// Module-level fan-out registry: one shared coreWS connection no matter how
// many components call useCoreStream. Handlers are grouped by topic so the
// single onFrame callback below can dispatch without every subscriber
// filtering frames itself.
const handlersByTopic = new Map<CoreStreamTopic, Set<Handler>>()
let closeSocket: (() => void) | null = null
let subscriberCount = 0

function dispatch(frame: CoreFrame) {
  const set = handlersByTopic.get(frame.topic as CoreStreamTopic)
  if (!set) return
  for (const handler of set) handler(frame.data)
}

function ensureSocket() {
  if (closeSocket) return
  closeSocket = coreWS(dispatch)
}

function releaseSocket() {
  if (closeSocket) {
    closeSocket()
    closeSocket = null
  }
}

// useCoreStream registers `handler` for `topic` frames on the shared coreWS
// socket: lazily connected on the first subscriber across the whole app and
// torn down when the last one unmounts. `handler` is kept in a ref so an
// identity change on every render (an inline arrow function, the common case)
// never tears down or reconnects the socket — only a `topic` change does.
export function useCoreStream(topic: CoreStreamTopic, handler: Handler): void {
  const handlerRef = useRef(handler)
  // Sync the ref after every render (not during) — keeps the latest handler
  // callable without the effect below depending on its identity.
  useEffect(() => {
    handlerRef.current = handler
  })

  useEffect(() => {
    const stable: Handler = (data) => handlerRef.current(data)

    let set = handlersByTopic.get(topic)
    if (!set) {
      set = new Set()
      handlersByTopic.set(topic, set)
    }
    set.add(stable)
    subscriberCount += 1
    ensureSocket()

    return () => {
      set.delete(stable)
      subscriberCount -= 1
      if (subscriberCount <= 0) {
        subscriberCount = 0
        releaseSocket()
      }
    }
  }, [topic])
}
