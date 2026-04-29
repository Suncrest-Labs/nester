"use client"
import Image from "next/image"
import * as React from "react"
import Link from "next/link"
import { Menu, ArrowRight } from "lucide-react"

import { Container } from "@/components/container"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTrigger } from "@/components/ui/sheet"
import Logo from "../../public/logo.png"
import { cn } from "@/lib/utils"

const navLinks = [
    { name: "Products", href: "#products" },
    { name: "Resources", href: "#resources" },
    { name: "Developers", href: "#developers" },
    { name: "About", href: "#about" },
    { name: "Docs", href: "/docs", newTab: true },
]

const PROD_DAPP_URL = "https://nesterdapp.netlify.app"
const LOCAL_DAPP_URL = process.env.NEXT_PUBLIC_DAPP_URL ?? "http://localhost:3001"

export function Navbar() {
    const [isScrolled, setIsScrolled] = React.useState(false)
    const [dappUrl, setDappUrl] = React.useState(PROD_DAPP_URL)

    React.useEffect(() => {
        const host = window.location.hostname
        setDappUrl(host === "localhost" || host === "127.0.0.1" ? LOCAL_DAPP_URL : PROD_DAPP_URL)
    }, [])

    React.useEffect(() => {
        const handleScroll = () => {
            setIsScrolled(window.scrollY > 20)
        }
        window.addEventListener("scroll", handleScroll, { passive: true })
        return () => window.removeEventListener("scroll", handleScroll)
    }, [])

    return (
        <nav
            className={cn(
                "fixed top-0 left-0 right-0 z-50 transition-all duration-300 border-b",
                isScrolled
                    ? "bg-white/80 backdrop-blur-md border-border py-4"
                    : "bg-transparent border-transparent py-6"
            )}
        >
            <Container className="flex items-center justify-between">
                <Link href="/" className="flex items-center gap-2 group">
                    <Image className="rounded-2xl" src={Logo} alt="Logo" width={40} height={40} />
                </Link>
                <div className="hidden lg:flex items-center gap-10">
                    <div className="flex items-center gap-8">
                        {navLinks.map((link) => (
                            <Link
                                key={link.name}
                                href={link.href}
                                {...("newTab" in link && link.newTab ? { target: "_blank", rel: "noopener noreferrer" } : {})}
                                className="text-[15px] font-medium text-foreground/70 hover:text-foreground transition-colors relative group py-2"
                            >
                                {link.name}
                            </Link>
                        ))}
                    </div>
                    <a href={dappUrl} target="_blank" rel="noopener noreferrer">
                        <Button
                            className="bg-[#0D0E1C] hover:bg-[#0D0E1C]/90 text-white rounded-full px-6 font-medium text-sm transition-all shadow-none"
                        >
                            Nester for Web
                        </Button>
                    </a>
                </div>

                <Sheet>
                    <SheetTrigger asChild>
                        <Button variant="ghost" size="icon" className="lg:hidden text-foreground hover:bg-black/5">
                            <Menu className="h-6 w-6" />
                            <span className="sr-only">Toggle menu</span>
                        </Button>
                    </SheetTrigger>
                    <SheetContent side="right" className="bg-white border-l border-border w-[300px] sm:w-[400px]">
                        <div className="flex flex-col h-full mt-8">
                            <div className="flex flex-col gap-6">
                                {navLinks.map((link) => (
                                    <Link
                                        key={link.name}
                                        href={link.href}
                                        {...("newTab" in link && link.newTab ? { target: "_blank", rel: "noopener noreferrer" } : {})}
                                        className="text-lg font-medium text-foreground hover:text-primary transition-colors flex items-center justify-between group"
                                    >
                                        {link.name}
                                        <ArrowRight className="w-4 h-4 opacity-0 -translate-x-2 group-hover:opacity-100 group-hover:translate-x-0 transition-all text-primary" />
                                    </Link>
                                ))}
                            </div>
                            <div className="mt-auto mb-8">
                                <a href={dappUrl} target="_blank" rel="noopener noreferrer" className="w-full">
                                    <Button className="w-full bg-[#0D0E1C] hover:bg-[#0D0E1C]/90 text-white rounded-full">
                                        Nester for Web
                                    </Button>
                                </a>
                            </div>
                        </div>
                    </SheetContent>
                </Sheet>
            </Container>
        </nav >
    )
}
