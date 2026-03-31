'use client'

import { useEffect, useRef, useState } from 'react'
import { Send, Sparkles, Loader2 } from 'lucide-react'
import { intelligence, type ChatMessage } from '@/lib/api/intelligence'
import { InsightCard, InsightCardSkeleton } from './insightCard'
import { MarketSentimentWidget } from './marketSentiment'
import type { PortfolioInsight } from '@/lib/api/intelligence'

// ── Quick prompts ─────────────────────────────────────────────────────────────

const QUICK_PROMPTS = [
  'What is my best vault for yield?',
  'Should I rebalance now?',
  'How is the market looking?',
  'Optimize my portfolio',
]

// ── QuickPrompts sub-component ────────────────────────────────────────────────

function QuickPrompts({ onSelect }: { onSelect: (prompt: string) => void }) {
  return (
    <div className="flex flex-wrap gap-2">
      {QUICK_PROMPTS.map((prompt) => (
        <button
          key={prompt}
          type="button"
          onClick={() => onSelect(prompt)}
          className="rounded-full border border-border bg-secondary/30 px-3 py-1.5 text-[11px] font-medium text-foreground/70 transition-all hover:border-black/15 hover:bg-secondary/60 hover:text-foreground"
        >
          {prompt}
        </button>
      ))}
    </div>
  )
}

// ── Chat message bubble ───────────────────────────────────────────────────────

function MessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === 'user'
  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      {!isUser && (
        <div className="mr-2 mt-1 flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-secondary">
          <Sparkles className="h-3 w-3 text-foreground/50" />
        </div>
      )}
      <div
        className={`max-w-[85%] rounded-2xl px-3.5 py-2.5 text-xs leading-relaxed ${
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

// ── PrometheusPanel ───────────────────────────────────────────────────────────

interface PrometheusPanelProps {
  userId: string
}

/**
 * PrometheusPanel
 *
 * Full AI advisory interface:
 * - Portfolio insight cards (fetched on mount)
 * - Market sentiment widget (auto-refreshes every 5 min)
 * - Conversational chat with streaming SSE responses
 * - Quick prompt shortcuts
 *
 * Degrades gracefully: insight fetch errors show a fallback message, chat
 * errors show an inline error bubble, SSE failures fall back silently.
 */
export function PrometheusPanel({ userId }: PrometheusPanelProps) {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [insights, setInsights] = useState<PortfolioInsight[]>([])
  const [insightsLoading, setInsightsLoading] = useState(true)
  const [insightsError, setInsightsError] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)

  // Fetch insight cards on mount
  useEffect(() => {
    intelligence
      .getPortfolioInsights(userId)
      .then(setInsights)
      .catch(() => setInsightsError(true))
      .finally(() => setInsightsLoading(false))
  }, [userId])

  // Scroll chat to bottom on new messages
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  // Cleanup SSE on unmount
  useEffect(() => {
    return () => eventSourceRef.current?.close()
  }, [])

  const sendMessage = (text: string) => {
    const trimmed = text.trim()
    if (!trimmed || streaming) return

    setInput('')
    setMessages((prev) => [...prev, { role: 'user', content: trimmed }])
    setStreaming(true)

    // Add an empty assistant message that will be filled by the stream
    setMessages((prev) => [...prev, { role: 'assistant', content: '' }])

    const source = intelligence.sendMessage(userId, trimmed)
    eventSourceRef.current = source

    source.onmessage = (e: MessageEvent) => {
      if (e.data === '[DONE]') {
        source.close()
        setStreaming(false)
        return
      }
      setMessages((prev) => {
        const updated = [...prev]
        const last = updated[updated.length - 1]
        if (last?.role === 'assistant') {
          updated[updated.length - 1] = {
            ...last,
            content: last.content + e.data,
          }
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
        // If the assistant message is still empty, replace with error
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
    <div className="flex flex-col gap-4">
      {/* Market sentiment */}
      <MarketSentimentWidget />

      {/* Insight cards */}
      <div>
        <p className="mb-2 text-xs font-medium text-foreground/50">Portfolio Insights</p>
        <div className="space-y-3">
          {insightsLoading ? (
            <>
              <InsightCardSkeleton />
              <InsightCardSkeleton />
            </>
          ) : insightsError ? (
            <div className="rounded-2xl border border-border bg-secondary/20 p-4">
              <p className="text-xs text-muted-foreground">
                Insights unavailable — intelligence service is offline.
              </p>
            </div>
          ) : insights.length === 0 ? (
            <div className="rounded-2xl border border-border bg-secondary/20 p-4">
              <p className="text-xs text-muted-foreground">
                No insights yet. Add a vault position to receive personalised analysis.
              </p>
            </div>
          ) : (
            insights.map((insight, i) => (
              <InsightCard key={i} {...insight} />
            ))
          )}
        </div>
      </div>

      {/* Chat */}
      <div className="flex flex-col rounded-2xl border border-border bg-white overflow-hidden">
        {/* Chat header */}
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <div className="flex h-6 w-6 items-center justify-center rounded-full bg-secondary">
            <Sparkles className="h-3 w-3 text-foreground/50" />
          </div>
          <p className="text-xs font-medium text-foreground">
            Ask <span className="font-display italic">Prometheus</span>
          </p>
          {streaming && (
            <Loader2 className="ml-auto h-3.5 w-3.5 animate-spin text-muted-foreground" />
          )}
        </div>

        {/* Messages */}
        <div className="flex max-h-64 min-h-[96px] flex-col gap-3 overflow-y-auto p-4">
          {messages.length === 0 ? (
            <p className="text-center text-xs text-muted-foreground">
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
        <div className="border-t border-border px-3 py-2.5 flex items-center gap-2">
          <input
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
      </div>
    </div>
  )
}