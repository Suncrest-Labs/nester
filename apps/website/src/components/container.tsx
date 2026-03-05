import { cn } from "@/lib/utils";

type ContainerProps = React.HTMLAttributes<HTMLDivElement>;

export function Container({ className, children, ...props }: ContainerProps) {
    return (
        <div
            className={cn("mx-auto w-full max-w-[1536px] px-4 md:px-8 lg:px-12 xl:px-16", className)}
            {...props}
        >
            {children}
        </div>
    );
}
