'use client'

import { useEffect, useRef, useState } from 'react'
import { Send, Sparkles, X } from 'lucide-react'
import { intelligence, type ChatMessage } from '@/lib/api/intelligence'
import { useWallet } from '@/components/wallet-provider'
import { motion, AnimatePresence } from 'framer-motion'

const QUICK_PROMPTS = [
  'What is my best vault for yield?',
  'Should I rebalance now?',
  'How is the market looking?',
  'Optimize my portfolio',
  'Recommend a vault for me',
]

function QuickPrompts({ onSelect }: { onSelect: (p: string) => void }) {
  return (
    <div className="flex flex-wrap gap-1.5" role="group" aria-label="Suggested questions">
      {QUICK_PROMPTS.map((p) => (
        <button
          key={p}
          type="button"
          onClick={() => onSelect(p)}
          className="rounded-full border border-black/10 bg-black/5 px-2.5 py-1 text-[10px] font-semibold text-black/70 transition-all hover:border-black/20 hover:bg-black/10 hover:text-black focus-visible:ring-2 focus-visible:ring-black"
        >
          {p}
        </button>
      ))}
    </div>
  )
}

function TypingDots() {
  return (
    <div className="flex items-center justify-start" aria-label="Prometheus is typing">
      <div className="mr-2 mt-1 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-black/5">
        <Sparkles className="h-2.5 w-2.5 text-black/50" aria-hidden="true" />
      </div>
      <div className="flex items-center gap-1 rounded-2xl border border-black/10 bg-white px-3 py-2.5">
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-black/40 [animation-delay:0ms]" />
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-black/40 [animation-delay:150ms]" />
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-black/40 [animation-delay:300ms]" />
      </div>
    </div>
  )
}

function renderBold(text: string): React.ReactNode[] {
  const parts = text.split(/(\*\*[^*]+\*\*)/)
  return parts.map((part, i) =>
    part.startsWith('**') && part.endsWith('**')
      ? <strong key={i}>{part.slice(2, -2)}</strong>
      : part
  )
}

function MessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === 'user'
  const paragraphs = message.content.split('\n').filter((p) => p.trim() !== '')
  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      {!isUser && (
        <div className="mr-2 mt-1 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-black/5">
          <Sparkles className="h-2.5 w-2.5 text-black/50" aria-hidden="true" />
        </div>
      )}
      <div
        className={`max-w-[85%] rounded-2xl px-3 py-2 text-[11px] leading-relaxed font-medium ${
          isUser
            ? 'bg-black text-white'
            : 'border border-black/10 bg-white text-black'
        }`}
      >
        {paragraphs.map((p, i) => (
          <p key={i} className={i > 0 ? 'mt-2' : ''}>
            {renderBold(p)}
          </p>
        ))}
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
  const panelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  useEffect(() => {
    return () => eventSourceRef.current?.close()
  }, [])

  useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 150)
      const handleEsc = (e: KeyboardEvent) => {
        if (e.key === 'Escape') setOpen(false)
      }
      window.addEventListener('keydown', handleEsc)
      return () => window.removeEventListener('keydown', handleEsc)
    }
  }, [open])

  // Focus trap
  useEffect(() => {
    if (!open || !panelRef.current) return
    const focusableElements = panelRef.current.querySelectorAll(
      'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
    )
    const firstElement = focusableElements[0] as HTMLElement
    const lastElement = focusableElements[focusableElements.length - 1] as HTMLElement

    const handleTab = (e: KeyboardEvent) => {
      if (e.key !== 'Tab') return
      if (e.shiftKey) {
        if (document.activeElement === firstElement) {
          lastElement.focus()
          e.preventDefault()
        }
      } else {
        if (document.activeElement === lastElement) {
          firstElement.focus()
          e.preventDefault()
        }
      }
    }
    window.addEventListener('keydown', handleTab)
    return () => window.removeEventListener('keydown', handleTab)
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
            ref={panelRef}
            initial={{ opacity: 0, y: 12, scale: 0.95 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 12, scale: 0.95 }}
            transition={{ duration: 0.2 }}
            className="flex w-85 flex-col overflow-hidden rounded-2xl border border-black/10 bg-white shadow-2xl shadow-black/10"
            role="dialog"
            aria-labelledby="chat-header"
            aria-modal="true"
          >
            {/* Header */}
            <div className="flex items-center gap-2 border-b border-black/10 bg-white px-4 py-3">
              <div className="flex h-6 w-6 items-center justify-center rounded-full bg-black/5">
                <Sparkles className="h-3 w-3 text-black/50" aria-hidden="true" />
              </div>
              <div className="flex-1">
                <p id="chat-header" className="text-xs font-semibold text-black">
                  <span className="font-display italic">Prometheus</span> AI
                </p>
                <p className="text-[10px] text-black/50 font-medium">DeFi Advisory</p>
              </div>
              <button
                onClick={() => setOpen(false)}
                aria-label="Close chat"
                className="flex h-6 w-6 items-center justify-center rounded-full text-black/40 transition-colors hover:bg-black/5 hover:text-black focus-visible:ring-2 focus-visible:ring-black"
              >
                <X className="h-3.5 w-3.5" aria-hidden="true" />
              </button>
            </div>

            {/* Messages */}
            <div 
                className="flex max-h-72 min-h-25 flex-col gap-3 overflow-y-auto p-4 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
                role="log"
                aria-live="polite"
            >
              {messages.length === 0 ? (
                <p className="text-center text-[11px] text-black/50 font-medium">
                  Ask me anything about your portfolio or DeFi markets.
                </p>
              ) : (
                messages.map((msg, i) => {
                  const isLastAndEmpty =
                    streaming && i === messages.length - 1 && msg.role === 'assistant' && msg.content === ''
                  return isLastAndEmpty ? (
                    <TypingDots key={i} />
                  ) : (
                    <MessageBubble key={i} message={msg} />
                  )
                })
              )}
              <div ref={messagesEndRef} />
            </div>

            {/* Quick prompts */}
            {messages.length === 0 && (
              <div className="border-t border-black/10 px-4 py-3">
                <QuickPrompts onSelect={sendMessage} />
              </div>
            )}

            {/* Input */}
            <div className="flex items-center gap-2 border-t border-black/10 px-3 py-2.5">
              <input
                ref={inputRef}
                type="text"
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && sendMessage(input)}
                placeholder="Ask Prometheus…"
                disabled={streaming}
                aria-label="Message Prometheus"
                className="flex-1 bg-transparent text-xs text-black font-medium placeholder:text-black/40 outline-none disabled:opacity-50"
              />
              <button
                type="button"
                onClick={() => sendMessage(input)}
                disabled={!input.trim() || streaming}
                aria-label="Send message"
                className="flex h-7 w-7 items-center justify-center rounded-full bg-black text-white transition-opacity disabled:opacity-30 focus-visible:ring-2 focus-visible:ring-black"
              >
                <Send className="h-3 w-3" aria-hidden="true" />
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Toggle button */}
      <button
        id="chat-toggle"
        onClick={() => setOpen((v) => !v)}
        className="flex h-13 w-13 items-center justify-center rounded-full bg-black text-white shadow-xl shadow-black/20 transition-transform hover:scale-105 active:scale-95 focus-visible:ring-2 focus-visible:ring-black focus-visible:ring-offset-2"
        aria-label="Toggle Prometheus AI chat"
        aria-expanded={open}
        aria-controls="chat-panel"
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
              <X className="h-5 w-5" aria-hidden="true" />
            </motion.span>
          ) : (
            <motion.span
              key="open"
              initial={{ rotate: 90, opacity: 0 }}
              animate={{ rotate: 0, opacity: 1 }}
              exit={{ rotate: -90, opacity: 0 }}
              transition={{ duration: 0.15 }}
            >
              <Sparkles className="h-5 w-5" aria-hidden="true" />
            </motion.span>
          )}
        </AnimatePresence>
      </button>
    </div>
  )
}
