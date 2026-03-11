import { cn } from "@/lib/utils"

export function Logo({ className }: { className?: string }) {
    return (
        <div className={cn("flex items-center gap-2", className)}>
            <div className="relative w-8 h-8 flex items-center justify-center bg-[#2B2D3C] rounded-full">
                <div className="w-3 h-3 bg-white rounded-full mb-1" />
            </div>
            <span className="font-heading font-bold text-2xl tracking-tight text-foreground lowercase">nester</span>
        </div>
    )
}
