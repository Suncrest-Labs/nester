import { docsContent, docsNav } from "../content";
import Link from "next/link";
import { BookOpen, Code, Server, Cpu, Rocket, Brain } from "lucide-react";
import { Metadata } from "next";

/* ──── Metadata ──── */

type Props = {
    params: Promise<{ slug?: string[] }>;
};

export async function generateMetadata({ params }: Props): Promise<Metadata> {
    const { slug: slugArray } = await params;
    const slug = slugArray?.[0];
    const doc = slug ? docsContent[slug] : null;

    if (!doc) {
        return {
            title: "Documentation",
            description: "Technical documentation for the Nester protocol.",
        };
    }

    return {
        title: doc.title,
        description: doc.content.slice(0, 160).replace(/[#*`>]/g, "").trim() + "...",
        alternates: {
            canonical: `/docs/${slug}`,
        },
    };
}

/* ──── Minimal Markdown Renderer ──── */

function renderMarkdown(md: string) {
    const lines = md.split("\n");
    const elements: React.ReactNode[] = [];
    let i = 0;
    let key = 0;

    while (i < lines.length) {
        const line = lines[i];

        // Code block
        if (line.startsWith("```")) {
            const lang = line.slice(3).trim();
            const codeLines: string[] = [];
            i++;
            while (i < lines.length && !lines[i].startsWith("```")) {
                codeLines.push(lines[i]);
                i++;
            }
            i++; // skip closing ```
            elements.push(
                <div key={key++} className="my-4 rounded-lg overflow-hidden border border-border">
                    {lang && (
                        <div className="px-4 py-1.5 text-[11px] font-mono text-muted-foreground bg-muted border-b border-border">
                            {lang}
                        </div>
                    )}
                    <pre className="px-4 py-3 overflow-x-auto bg-muted text-[13px] leading-relaxed font-mono text-foreground/80">
                        <code>{codeLines.join("\n")}</code>
                    </pre>
                </div>
            );
            continue;
        }

        // Headings
        if (line.startsWith("# ")) {
            elements.push(<h1 key={key++} className="text-3xl font-heading font-bold tracking-tight mt-8 mb-4 first:mt-0">{renderInline(line.slice(2))}</h1>);
            i++; continue;
        }
        if (line.startsWith("## ")) {
            elements.push(<h2 key={key++} className="text-xl font-heading font-semibold tracking-tight mt-10 mb-3 pb-2 border-b border-border">{renderInline(line.slice(3))}</h2>);
            i++; continue;
        }
        if (line.startsWith("### ")) {
            elements.push(<h3 key={key++} className="text-lg font-heading font-semibold mt-8 mb-2">{renderInline(line.slice(4))}</h3>);
            i++; continue;
        }

        // Blockquote
        if (line.startsWith("> ")) {
            elements.push(
                <blockquote key={key++} className="my-4 pl-4 border-l-4 border-primary text-muted-foreground italic">
                    {renderInline(line.slice(2))}
                </blockquote>
            );
            i++; continue;
        }

        // Table
        if (line.includes("|") && i + 1 < lines.length && lines[i + 1]?.includes("---")) {
            const headers = line.split("|").filter(Boolean).map((h) => h.trim());
            i += 2; // skip header + separator
            const rows: string[][] = [];
            while (i < lines.length && lines[i].includes("|")) {
                rows.push(lines[i].split("|").filter(Boolean).map((c) => c.trim()));
                i++;
            }
            elements.push(
                <div key={key++} className="my-4 overflow-x-auto rounded-lg border border-border">
                    <table className="w-full text-sm">
                        <thead>
                            <tr className="bg-muted">
                                {headers.map((h, hi) => (
                                    <th key={hi} className="px-4 py-2.5 text-left font-semibold text-foreground/80 border-b border-border">
                                        {renderInline(h)}
                                    </th>
                                ))}
                            </tr>
                        </thead>
                        <tbody>
                            {rows.map((row, ri) => (
                                <tr key={ri} className="border-b last:border-b-0 border-border">
                                    {row.map((cell, ci) => (
                                        <td key={ci} className="px-4 py-2.5 text-muted-foreground">
                                            {renderInline(cell)}
                                        </td>
                                    ))}
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            );
            continue;
        }

        // Unordered list
        if (line.startsWith("- ") || line.startsWith("* ")) {
            const items: string[] = [];
            while (i < lines.length && (lines[i].startsWith("- ") || lines[i].startsWith("* "))) {
                items.push(lines[i].slice(2));
                i++;
            }
            elements.push(
                <ul key={key++} className="my-3 ml-4 space-y-1.5 list-disc list-outside text-muted-foreground text-[14px] leading-relaxed">
                    {items.map((item, ii) => (
                        <li key={ii}>{renderInline(item)}</li>
                    ))}
                </ul>
            );
            continue;
        }

        // Empty line
        if (line.trim() === "") {
            i++; continue;
        }

        // Paragraph
        elements.push(
            <p key={key++} className="my-3 text-[14px] leading-relaxed text-muted-foreground">
                {renderInline(line)}
            </p>
        );
        i++;
    }

    return elements;
}

function renderInline(text: string): React.ReactNode {
    // Process inline code, bold, and inline backticks
    const parts: React.ReactNode[] = [];
    let remaining = text;
    let idx = 0;

    while (remaining.length > 0) {
        // Inline code
        const codeMatch = remaining.match(/^(.*?)`([^`]+)`(.*)$/);
        if (codeMatch) {
            if (codeMatch[1]) parts.push(<span key={idx++}>{processEmphasis(codeMatch[1])}</span>);
            parts.push(
                <code key={idx++} className="px-1.5 py-0.5 text-[13px] font-mono bg-muted text-primary rounded">
                    {codeMatch[2]}
                </code>
            );
            remaining = codeMatch[3];
            continue;
        }

        parts.push(<span key={idx++}>{processEmphasis(remaining)}</span>);
        break;
    }

    return parts.length === 1 ? parts[0] : <>{parts}</>;
}

function processEmphasis(text: string): React.ReactNode {
    // Bold
    const parts = text.split(/\*\*(.*?)\*\*/g);
    if (parts.length === 1) return text;
    return (
        <>
            {parts.map((part, i) =>
                i % 2 === 1 ? <strong key={i} className="font-semibold text-foreground">{part}</strong> : part
            )}
        </>
    );
}

/* ──── Prev/Next Navigation ──── */

function getAdjacentPages(slug: string) {
    const allPages = docsNav.flatMap((s) => s.children || []);
    const idx = allPages.findIndex((p) => p.slug === slug);
    return {
        prev: idx > 0 ? allPages[idx - 1] : null,
        next: idx < allPages.length - 1 ? allPages[idx + 1] : null,
    };
}

/* ──── Page Component ──── */

const landingSections = [
    { icon: BookOpen, title: "Getting Started", desc: "Learn what Nester is, how the architecture works, and set up your local environment.", links: [{ title: "Introduction", slug: "introduction" }, { title: "Architecture Overview", slug: "architecture" }, { title: "Quick Start", slug: "quick-start" }] },
    { icon: Rocket, title: "Core Concepts", desc: "Understand the four pillars: savings vaults, multi-asset yield, instant off-ramps, and AI intelligence.", links: [{ title: "Savings Layer", slug: "savings-layer" }, { title: "Yield Layer", slug: "yield-layer" }, { title: "Off-Ramp Layer", slug: "offramp-layer" }, { title: "AI Intelligence", slug: "ai-layer" }] },
    { icon: Code, title: "Smart Contracts", desc: "Soroban/Rust contract reference for vaults, share tokens, yield adapters, and escrow.", links: [{ title: "Contracts Overview", slug: "contracts-overview" }, { title: "Vault Contract", slug: "vault-contract" }, { title: "Yield Adapters", slug: "yield-adapters" }, { title: "Escrow & Settlement", slug: "escrow" }] },
    { icon: Server, title: "Backend API", desc: "Go + Chi REST API reference with request/response examples for all endpoints.", links: [{ title: "API Overview", slug: "api-overview" }, { title: "Vault Endpoints", slug: "vault-api" }, { title: "Off-Ramp Endpoints", slug: "offramp-api" }] },
    { icon: Cpu, title: "Frontend SDK", desc: "Wallet integration, transaction signing, and frontend patterns for the DApp.", links: [{ title: "Wallet Integration", slug: "wallet-integration" }, { title: "Transaction Signing", slug: "transaction-signing" }] },
    { icon: Brain, title: "Prometheus AI", desc: "Python/FastAPI AI service powered by Claude for portfolio intelligence.", links: [{ title: "Prometheus Overview", slug: "prometheus-overview" }, { title: "API Reference", slug: "prometheus-api" }] },
];

function DocsLanding() {
    return (
        <div>
            <h1 className="text-3xl font-heading font-bold tracking-tight mb-3">Nester Documentation</h1>
            <p className="text-muted-foreground text-[15px] mb-10 max-w-2xl">
                Technical documentation for the Nester protocol — decentralized savings, yield optimization, instant fiat off-ramps, and AI-powered portfolio intelligence on Stellar/Soroban.
            </p>
            <div className="grid gap-6 sm:grid-cols-2">
                {landingSections.map((section) => (
                    <div key={section.title} className="p-5 rounded-xl border border-border hover:border-primary/30 transition-colors">
                        <div className="flex items-center gap-2.5 mb-3">
                            <section.icon className="w-5 h-5 text-primary" />
                            <h2 className="font-heading font-semibold text-[15px]">{section.title}</h2>
                        </div>
                        <p className="text-sm text-muted-foreground mb-4 leading-relaxed">{section.desc}</p>
                        <div className="space-y-1">
                            {section.links.map((link) => (
                                <Link key={link.slug} href={`/docs/${link.slug}`} className="block text-sm text-primary hover:text-primary/80 transition-colors">
                                    &bull; {link.title}
                                </Link>
                            ))}
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
}

export default async function DocsPage({ params }: Props) {
    const { slug: slugArray } = await params;

    // No slug = landing page
    if (!slugArray || slugArray.length === 0) {
        return <DocsLanding />;
    }

    const slug = slugArray[0];
    const doc = docsContent[slug];
    const { prev, next } = getAdjacentPages(slug);

    if (!doc) {
        return (
            <div className="py-20 text-center">
                <h1 className="text-2xl font-heading font-bold mb-4">Page not found</h1>
                <p className="text-muted-foreground mb-6">The documentation page &quot;{slug}&quot; doesn&apos;t exist yet.</p>
                <Link href="/docs/introduction" className="text-primary hover:underline">
                    Go to Introduction
                </Link>
            </div>
        );
    }

    return (
        <article>
            {/* Content */}
            <div className="docs-content">{renderMarkdown(doc.content)}</div>

            {/* Prev / Next */}
            <div className="mt-16 pt-6 border-t border-border flex items-center justify-between gap-4">
                {prev ? (
                    <Link
                        href={`/docs/${prev.slug}`}
                        className="group flex flex-col items-start px-4 py-3 rounded-lg border border-border hover:border-primary/30 transition-colors flex-1"
                    >
                        <span className="text-[11px] text-muted-foreground mb-1">Previous</span>
                        <span className="text-sm font-medium text-foreground/80 group-hover:text-primary transition-colors">
                            &laquo; {prev.title}
                        </span>
                    </Link>
                ) : <div />}
                {next ? (
                    <Link
                        href={`/docs/${next.slug}`}
                        className="group flex flex-col items-end px-4 py-3 rounded-lg border border-border hover:border-primary/30 transition-colors flex-1"
                    >
                        <span className="text-[11px] text-muted-foreground mb-1">Next</span>
                        <span className="text-sm font-medium text-foreground/80 group-hover:text-primary transition-colors">
                            {next.title} &raquo;
                        </span>
                    </Link>
                ) : <div />}
            </div>
        </article>
    );
}

