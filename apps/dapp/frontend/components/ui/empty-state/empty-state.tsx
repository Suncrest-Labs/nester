"use client";

import React from "react";
import { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface EmptyStateProps {
  icon?: LucideIcon;
  title: string;
  description: string;
  action?: {
    label: string;
    onClick: () => void;
    variant?: "primary" | "secondary";
  };
  illustration?: React.ReactNode;
  className?: string;
  size?: "sm" | "default" | "lg";
}

export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  illustration,
  className,
  size = "default"
}: EmptyStateProps) {
  const sizeClasses = {
    sm: {
      container: "py-8",
      iconSize: "h-8 w-8",
      iconContainer: "h-12 w-12",
      title: "text-sm",
      description: "text-xs",
      button: "px-3 py-1.5 text-xs"
    },
    default: {
      container: "py-12",
      iconSize: "h-10 w-10",
      iconContainer: "h-16 w-16",
      title: "text-base",
      description: "text-sm",
      button: "px-4 py-2 text-sm"
    },
    lg: {
      container: "py-16",
      iconSize: "h-12 w-12",
      iconContainer: "h-20 w-20",
      title: "text-lg",
      description: "text-base",
      button: "px-5 py-2.5 text-sm"
    }
  };

  const classes = sizeClasses[size];

  return (
    <div className={cn(
      "flex flex-col items-center justify-center text-center",
      classes.container,
      className
    )}>
      {illustration || (Icon && (
        <div className={cn(
          "flex items-center justify-center rounded-full bg-black/[0.04] dark:bg-white/[0.04] mb-4",
          classes.iconContainer
        )}>
          <Icon className={cn("text-black/40 dark:text-white/40", classes.iconSize)} />
        </div>
      ))}

      <h3 className={cn("font-medium text-black dark:text-white mb-2", classes.title)}>
        {title}
      </h3>

      <p className={cn("text-black/60 dark:text-white/60 max-w-sm mb-6", classes.description)}>
        {description}
      </p>
      
      {action && (
        <button
          onClick={action.onClick}
          className={cn(
            "rounded-lg font-medium transition-all",
            classes.button,
            action.variant === "primary" 
              ? "bg-black dark:bg-blue-600 text-white hover:bg-black/90 dark:hover:bg-blue-700"
              : "border border-black/[0.08] dark:border-white/[0.08] bg-white dark:bg-[#100F0F] text-black dark:text-white hover:border-black/20 dark:hover:border-white/20 hover:bg-black/[0.02] dark:hover:bg-white/[0.02]"
          )}
        >
          {action.label}
        </button>
      )}
    </div>
  );
}