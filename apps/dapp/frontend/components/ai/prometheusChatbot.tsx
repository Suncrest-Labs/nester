'use client'

import { useEffect, useRef, useState } from 'react'
import { Send, Sparkles, X } from 'lucide-react'
import { intelligence, type ChatMessage } from '@/lib/api/intelligence'
import { useWallet } from '@/components/wallet-provider'
import { usePortfolio } from '@/components/portfolio-provider'
import { motion, AnimatePresence } from 'framer-motion'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

const QUICK_PROMPTS = [
  'Which vault should I use for $5,000 with low risk?',
  'Should I rebalance my portfolio?',
  'What is happening in the DeFi market this week?',
  'My XLM is up 40%, should I take profits?',
]

interface AssistantMessage extends ChatMessage {
  actions?: Array<{ label: string; href: string }>
  confidence?: number
  riskScore?: number
}

const storageKey = (address: string) => `nester_prometheus_chat_v1:${address}`

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

function TypingDots() {
  return (
    <div className="flex items-center justify-start">
      <div className="mr-2 mt-1 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-secondary">
        <Sparkles className="h-2.5 w-2.5 text-foreground/50" />
      </div>
      <div className="flex items-center gap-1 rounded-2xl border border-border bg-white px-3 py-2.5">
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-foreground/40 [animation-delay:0ms]" />
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-foreground/40 [animation-delay:150ms]" />
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-foreground/40 [animation-delay:300ms]" />
      </div>
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
        {isUser ? (
          <p>{message.content}</p>
        ) : (
          <div className="prose prose-sm max-w-none prose-p:my-1 prose-li:my-0 prose-ul:my-1">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>
              {message.content}
            </ReactMarkdown>
          </div>
        )}
      </div>
    </div>
  )
}

export function PrometheusChatbot() {
  const { isConnected, address } = useWallet()
  const { positions, balances } = usePortfolio()
  const [open, setOpen] = useState(false)
  const [messages, setMessages] = useState<AssistantMessage[]>([])
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 150)
    }
  }, [open])

  useEffect(() => {
    if (!address || typeof window === 'undefined') return
    const raw = sessionStorage.getItem(storageKey(address))
    if (!raw) return
    try {
      const saved = JSON.parse(raw) as AssistantMessage[]
      if (Array.isArray(saved)) setMessages(saved)
    } catch {
      // Ignore invalid chat payloads.
    }
  }, [address])

  useEffect(() => {
    if (!address || typeof window === 'undefined') return
    sessionStorage.setItem(storageKey(address), JSON.stringify(messages))
  }, [address, messages])

  useEffect(() => {
    const openHandler = (event: Event) => {
      const customEvent = event as CustomEvent<{ prompt?: string }>
      setOpen(true)
      if (customEvent.detail?.prompt) {
        setInput(customEvent.detail.prompt)
      }
      setTimeout(() => inputRef.current?.focus(), 100)
    }
    window.addEventListener('nester:prometheus-open', openHandler)
    return () => window.removeEventListener('nester:prometheus-open', openHandler)
  }, [])

  if (!isConnected || !address) return null

  const sendMessage = (text: string) => {
    const trimmed = text.trim()
    if (!trimmed || streaming) return

    setInput('')
    setMessages((prev) => [...prev, { role: 'user', content: trimmed }])
    setStreaming(true)
    setMessages((prev) => [...prev, { role: 'assistant', content: '' }])

    intelligence
      .sendMessage(trimmed, {
        walletAddress: address,
        balances,
        positions: positions.map((p) => ({
          vaultId: p.vaultId,
          vaultName: p.vaultName,
          asset: p.asset,
          currentValue: p.currentValue,
          apy: p.apy,
          yieldEarned: p.yieldEarned,
        })),
      })
      .then((response) => {
        setMessages((prev) => {
          const updated = [...prev]
          const last = updated[updated.length - 1]
          if (last?.role === 'assistant') {
            updated[updated.length - 1] = {
              ...last,
              content: response.content,
              actions: response.actions,
              confidence: response.confidence,
              riskScore: response.riskScore,
            }
          }
          return updated
        })
      })
      .catch(() => {
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
      })
      .finally(() => setStreaming(false))
  }

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col items-end gap-3 sm:bottom-6 sm:right-6">
      <AnimatePresence>
        {open && (
          <motion.div
            initial={{ opacity: 0, y: 12, scale: 0.95 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 12, scale: 0.95 }}
            transition={{ duration: 0.2 }}
            className="fixed inset-0 z-50 flex h-[100dvh] w-full flex-col overflow-hidden rounded-none border border-border bg-white shadow-2xl shadow-black/10 sm:static sm:h-auto sm:max-h-[84dvh] sm:min-h-[28rem] sm:w-85 sm:rounded-2xl"
          >
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
              <button
                onClick={() => setOpen(false)}
                className="flex h-6 w-6 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>

            <div className="flex flex-1 min-h-25 flex-col gap-3 overflow-y-auto p-4 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
              {messages.length === 0 ? (
                <p className="text-center text-[11px] text-muted-foreground">
                  Ask me anything about your portfolio or DeFi markets.
                </p>
              ) : (
                messages.map((msg, i) => {
                  const isLastAndEmpty =
                    streaming && i === messages.length - 1 && msg.role === 'assistant' && msg.content === ''
                  return isLastAndEmpty ? (
                    <TypingDots key={i} />
                  ) : (
                    <div key={i} className="space-y-2">
                      <MessageBubble message={msg} />
                      {msg.role === 'assistant' && msg.actions && msg.actions.length > 0 && (
                        <div className="ml-7 flex flex-wrap gap-2">
                          {msg.actions.map((action, idx) => (
                            <a
                              key={`${action.href}-${idx}`}
                              href={action.href}
                              className="rounded-full border border-border bg-white px-2.5 py-1 text-[10px] font-medium text-foreground/75 transition-colors hover:border-black/20 hover:text-foreground"
                            >
                              {action.label}
                            </a>
                          ))}
                        </div>
                      )}
                      {msg.role === 'assistant' && typeof msg.confidence === 'number' && (
                        <p className="ml-7 text-[10px] text-muted-foreground">
                          Confidence: {Math.round(msg.confidence * 100)}%
                          {typeof msg.riskScore === 'number' ? ` · Risk score: ${Math.round(msg.riskScore * 100)}` : ''}
                        </p>
                      )}
                    </div>
                  )
                })
              )}
              <div ref={messagesEndRef} />
            </div>

            {messages.length === 0 && (
              <div className="border-t border-border px-4 py-3">
                <QuickPrompts onSelect={sendMessage} />
              </div>
            )}

            <div className="flex items-center gap-2 border-t border-border px-3 py-2.5">
              <input
                ref={inputRef}
                type="text"
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && sendMessage(input)}
                placeholder="Ask Prometheus..."
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
