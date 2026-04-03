'use client'

import { useEffect, useRef, useState } from 'react'
import { Send, Sparkles, Loader2, X } from 'lucide-react'
import { intelligence, type ChatMessage } from '@/lib/api/intelligence'
import { useWallet } from '@/components/wallet-provider'
import { motion, AnimatePresence } from 'framer-motion'

const QUICK_PROMPTS = [
  'What is my best vault for yield?',
  'Should I rebalance now?',
  'How is the market looking?',
  'Optimize my portfolio',
]

function QuickPrompts({ onSelect }: { onSelect: (p: string) => void }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {QUICK_PROMPTS.map((p) => (
        <button
          key={p}
          type="button"
          onClick={() => onSelect(p)}
          className="rounded-full border border-border bg-secondary/30 px-2.5 py-1 text-[10px] font-medium text-foreground/70 transition-all hover:border-black/15 hover:bg-secondary/60 hover:text-foreground"
        >
          {p}
        </button>
      ))}
    </div>
  )
}

function MessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === 'user'
  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      {!isUser && (
        <div className="mr-2 mt-1 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-secondary">
          <Sparkles className="h-2.5 w-2.5 text-foreground/50" />
        </div>
      )}
      <div
        className={`max-w-[85%] rounded-2xl px-3 py-2 text-[11px] leading-relaxed ${
          isUser
            ? 'bg-foreground text-background'
            : 'border border-border bg-white text-foreground'
        }`}
      >
        {message.content}
      </div>
    </div>
  )
}

export function PrometheusChatbot() {
  const { isConnected, address } = useWallet()
  const [open, setOpen] = useState(false)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  useEffect(() => {
    return () => eventSourceRef.current?.close()
  }, [])

  useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 150)
    }
  }, [open])

  if (!isConnected || !address) return null

  const sendMessage = (text: string) => {
    const trimmed = text.trim()
    if (!trimmed || streaming) return

    setInput('')
    setMessages((prev) => [...prev, { role: 'user', content: trimmed }])
    setStreaming(true)
    setMessages((prev) => [...prev, { role: 'assistant', content: '' }])

    const source = intelligence.sendMessage(address, trimmed)
    eventSourceRef.current = source

    source.onmessage = (e: MessageEvent) => {
      if (e.data === '[DONE]') {
        source.close()
        setStreaming(false)
        return
      }
      const chunk = e.data.replace(/\\n/g, '\n')
      setMessages((prev) => {
        const updated = [...prev]
        const last = updated[updated.length - 1]
        if (last?.role === 'assistant') {
          updated[updated.length - 1] = { ...last, content: last.content + chunk }
        }
        return updated
      })
    }

    source.onerror = () => {
      source.close()
      setStreaming(false)
      setMessages((prev) => {
        const updated = [...prev]
        const last = updated[updated.length - 1]
        if (last?.role === 'assistant' && last.content === '') {
          updated[updated.length - 1] = {
            ...last,
            content: 'Sorry, I had trouble connecting. Please try again.',
          }
        }
        return updated
      })
    }
  }

  return (
    <div className="fixed bottom-6 right-6 z-50 flex flex-col items-end gap-3">
      <AnimatePresence>
        {open && (
          <motion.div
            initial={{ opacity: 0, y: 12, scale: 0.95 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 12, scale: 0.95 }}
            transition={{ duration: 0.2 }}
            className="flex w-85 flex-col overflow-hidden rounded-2xl border border-border bg-white shadow-2xl shadow-black/10"
          >
            {/* Header */}
            <div className="flex items-center gap-2 border-b border-border bg-white px-4 py-3">
              <div className="flex h-6 w-6 items-center justify-center rounded-full bg-secondary">
                <Sparkles className="h-3 w-3 text-foreground/50" />
              </div>
              <div className="flex-1">
                <p className="text-xs font-medium text-foreground">
                  <span className="font-display italic">Prometheus</span> AI
                </p>
                <p className="text-[10px] text-muted-foreground">DeFi Advisory</p>
              </div>
              {streaming && (
                <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
              )}
              <button
                onClick={() => setOpen(false)}
                className="flex h-6 w-6 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>

            {/* Messages */}
            <div className="flex max-h-72 min-h-25 flex-col gap-3 overflow-y-auto p-4 scrollbar-thin">
              {messages.length === 0 ? (
                <p className="text-center text-[11px] text-muted-foreground">
                  Ask me anything about your portfolio or DeFi markets.
                </p>
              ) : (
                messages.map((msg, i) => <MessageBubble key={i} message={msg} />)
              )}
              <div ref={messagesEndRef} />
            </div>

            {/* Quick prompts */}
            {messages.length === 0 && (
              <div className="border-t border-border px-4 py-3">
                <QuickPrompts onSelect={sendMessage} />
              </div>
            )}

            {/* Input */}
            <div className="flex items-center gap-2 border-t border-border px-3 py-2.5">
              <input
                ref={inputRef}
                type="text"
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && sendMessage(input)}
                placeholder="Ask Prometheus…"
                disabled={streaming}
                className="flex-1 bg-transparent text-xs text-foreground placeholder:text-muted-foreground outline-none disabled:opacity-50"
              />
              <button
                type="button"
                onClick={() => sendMessage(input)}
                disabled={!input.trim() || streaming}
                aria-label="Send message"
                className="flex h-7 w-7 items-center justify-center rounded-full bg-foreground text-background transition-opacity disabled:opacity-30"
              >
                <Send className="h-3 w-3" />
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Toggle button */}
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex h-13 w-13 items-center justify-center rounded-full bg-foreground text-background shadow-xl shadow-black/20 transition-transform hover:scale-105 active:scale-95"
        aria-label="Toggle Prometheus AI chat"
      >
        <AnimatePresence mode="wait" initial={false}>
          {open ? (
            <motion.span
              key="close"
              initial={{ rotate: -90, opacity: 0 }}
              animate={{ rotate: 0, opacity: 1 }}
              exit={{ rotate: 90, opacity: 0 }}
              transition={{ duration: 0.15 }}
            >
              <X className="h-5 w-5" />
            </motion.span>
          ) : (
            <motion.span
              key="open"
              initial={{ rotate: 90, opacity: 0 }}
              animate={{ rotate: 0, opacity: 1 }}
              exit={{ rotate: -90, opacity: 0 }}
              transition={{ duration: 0.15 }}
            >
              <Sparkles className="h-5 w-5" />
            </motion.span>
          )}
        </AnimatePresence>
      </button>
    </div>
  )
}
