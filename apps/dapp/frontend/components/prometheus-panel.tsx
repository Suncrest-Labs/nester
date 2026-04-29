"use client";

import { useState, useRef, useEffect } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { 
    MessageSquare, 
    Send, 
    X, 
    Bot, 
    User, 
    Sparkles, 
    Loader2
} from "lucide-react";
import { cn } from "@/lib/utils";
import { usePortfolio } from "@/components/portfolio-provider";

type Message = {
    role: "user" | "assistant";
    content: string;
};

export function PrometheusPanel() {
    const [isOpen, setIsOpen] = useState(false);
    const [messages, setMessages] = useState<Message[]>([]);
    const [input, setInput] = useState("");
    const [isStreaming, setIsStreaming] = useState(false);
    const [sessionId, setSessionId] = useState("");
    const scrollRef = useRef<HTMLDivElement>(null);
    const { positions } = usePortfolio();
    const totalValue = positions.reduce((sum, p) => sum + p.currentValue, 0);

    useEffect(() => {
        setSessionId(crypto.randomUUID());
    }, []);

    useEffect(() => {
        if (scrollRef.current) {
            scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
        }
    }, [messages]);

    const handleSend = async () => {
        if (!input.trim() || isStreaming) return;

        const userMessage = input.trim();
        setInput("");
        setMessages(prev => [...prev, { role: "user", content: userMessage }]);
        setIsStreaming(true);

        // Placeholder for assistant message that will be updated
        setMessages(prev => [...prev, { role: "assistant", content: "" }]);

        try {
            const response = await fetch("http://localhost:8000/chat", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                    message: userMessage,
                    session_id: sessionId,
                    context: {
                        portfolio_value: totalValue,
                        vaults: positions.map(p => ({ name: p.vaultName, value: p.currentValue })),
                        risk_profile: "moderate", // Default or from user settings
                        savings_goal: "wealth_accumulation"
                    }
                })
            });

            if (!response.body) throw new Error("No response body");

            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            let fullContent = "";

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                const chunk = decoder.decode(value);
                const lines = chunk.split("\n");

                for (const line of lines) {
                    if (line.startsWith("data: ")) {
                        const data = line.slice(6);
                        if (data === "[DONE]") continue;
                        
                        try {
                            const parsed = JSON.parse(data);
                            if (parsed.token) {
                                fullContent += parsed.token;
                                setMessages(prev => {
                                    const next = [...prev];
                                    next[next.length - 1].content = fullContent;
                                    return next;
                                });
                            } else if (parsed.error) {
                                throw new Error(parsed.error);
                            }
                        } catch (e) {
                            console.error("Error parsing SSE chunk", e);
                        }
                    }
                }
            }
        } catch (err) {
            console.error("Chat error:", err);
            setMessages(prev => {
                const next = [...prev];
                next[next.length - 1].content = "Sorry, I encountered an error. Please try again.";
                return next;
            });
        } finally {
            setIsStreaming(false);
        }
    };

    return (
        <>
            {/* Floating Toggle Button */}
            <button
                onClick={() => setIsOpen(true)}
                className={cn(
                    "fixed bottom-8 right-8 z-50 flex h-14 w-14 items-center justify-center rounded-full bg-brand-dark text-white shadow-lg transition-transform hover:scale-110 active:scale-95",
                    isOpen && "scale-0 opacity-0"
                )}
            >
                <Sparkles className="h-6 w-6" />
            </button>

            <AnimatePresence>
                {isOpen && (
                    <motion.div
                        initial={{ opacity: 0, scale: 0.9, y: 20 }}
                        animate={{ opacity: 1, scale: 1, y: 0 }}
                        exit={{ opacity: 0, scale: 0.9, y: 20 }}
                        className="fixed bottom-8 right-8 z-[60] flex h-[600px] w-[400px] flex-col overflow-hidden rounded-3xl border border-white/10 bg-white shadow-2xl shadow-black/20"
                    >
                        {/* Header */}
                        <div className="flex items-center justify-between border-b border-border bg-brand-dark px-6 py-4 text-white">
                            <div className="flex items-center gap-3">
                                <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-white/10">
                                    <Sparkles className="h-5 w-5 text-brand-light" />
                                </div>
                                <div>
                                    <h3 className="font-heading text-sm font-medium">Prometheus AI</h3>
                                    <p className="text-[10px] uppercase tracking-wider opacity-60">Personal Assistant</p>
                                </div>
                            </div>
                            <button
                                onClick={() => setIsOpen(false)}
                                className="rounded-full p-1.5 opacity-60 transition-opacity hover:opacity-100"
                            >
                                <X className="h-5 w-5" />
                            </button>
                        </div>

                        {/* Messages Area */}
                        <div 
                            ref={scrollRef}
                            className="flex-1 overflow-y-auto bg-slate-50 p-6 space-y-6"
                        >
                            {messages.length === 0 && (
                                <div className="flex flex-col items-center justify-center h-full text-center space-y-4">
                                    <div className="h-12 w-12 rounded-2xl bg-brand-dark/5 flex items-center justify-center text-brand-dark">
                                        <Bot className="h-6 w-6" />
                                    </div>
                                    <div>
                                        <p className="font-medium text-foreground">Welcome to Prometheus</p>
                                        <p className="text-xs text-muted-foreground mt-1 max-w-[200px]">
                                            Ask me about your portfolio, vault yields, or savings goals.
                                        </p>
                                    </div>
                                </div>
                            )}
                            
                            {messages.map((m, i) => (
                                <div 
                                    key={i}
                                    className={cn(
                                        "flex gap-3",
                                        m.role === "user" ? "flex-row-reverse" : "flex-row"
                                    )}
                                >
                                    <div className={cn(
                                        "flex h-8 w-8 shrink-0 items-center justify-center rounded-full border",
                                        m.role === "user" ? "bg-white" : "bg-brand-dark text-white"
                                    )}>
                                        {m.role === "user" ? <User className="h-4 w-4" /> : <Bot className="h-4 w-4" />}
                                    </div>
                                    <div className={cn(
                                        "max-w-[80%] rounded-2xl px-4 py-3 text-sm shadow-sm",
                                        m.role === "user" 
                                            ? "bg-brand-dark text-white rounded-tr-none" 
                                            : "bg-white text-foreground border border-border rounded-tl-none"
                                    )}>
                                        {m.content || (
                                            <div className="flex gap-1 py-1">
                                                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-brand-dark/40" />
                                                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-brand-dark/40 [animation-delay:0.2s]" />
                                                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-brand-dark/40 [animation-delay:0.4s]" />
                                            </div>
                                        )}
                                    </div>
                                </div>
                            ))}
                        </div>

                        {/* Input Area */}
                        <div className="border-t border-border bg-white p-4">
                            <div className="flex items-center gap-2 rounded-2xl border border-border bg-slate-50 px-4 py-2 focus-within:border-brand-dark/30 transition-colors">
                                <input
                                    type="text"
                                    value={input}
                                    onChange={(e) => setInput(e.target.value)}
                                    onKeyDown={(e) => e.key === "Enter" && handleSend()}
                                    placeholder="Type your message..."
                                    className="flex-1 bg-transparent py-2 text-sm outline-none placeholder:text-muted-foreground/50"
                                />
                                <button
                                    onClick={handleSend}
                                    disabled={!input.trim() || isStreaming}
                                    className="flex h-8 w-8 items-center justify-center rounded-xl bg-brand-dark text-white disabled:opacity-40 transition-transform active:scale-90"
                                >
                                    {isStreaming ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
                                </button>
                            </div>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </>
    );
}
