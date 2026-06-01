import { ReactNode } from "react";

interface CardProps {
  children: ReactNode;
  className?: string;
}

export default function Card({ children, className = "" }: CardProps) {
  return (
    <div className={`border rounded-xl p-6 ${className}`}>
      {children}
    </div>
  );
}
export { Card };