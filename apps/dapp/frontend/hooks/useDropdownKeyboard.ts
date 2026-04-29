import { KeyboardEvent, useEffect, useRef, RefObject } from 'react';

interface UseDropdownKeyboardProps {
    isOpen: boolean;
    setIsOpen: (isOpen: boolean) => void;
    itemCount: number;
    onSelect: (index: number) => void;
}

export function useDropdownKeyboard({ isOpen, setIsOpen, itemCount, onSelect }: UseDropdownKeyboardProps) {
    const listboxRef = useRef<HTMLDivElement>(null);
    const triggerRef = useRef<HTMLButtonElement>(null);

    const handleTriggerKeyDown = (e: KeyboardEvent) => {
        if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown') {
            e.preventDefault();
            setIsOpen(true);
        }
    };

    const handleListboxKeyDown = (e: KeyboardEvent) => {
        if (!isOpen) return;

        const focusableItems = listboxRef.current?.querySelectorAll('[role="option"]');
        if (!focusableItems || focusableItems.length === 0) return;

        const currentIndex = Array.from(focusableItems).findIndex(item => item === document.activeElement);

        switch (e.key) {
            case 'ArrowDown':
                e.preventDefault();
                if (currentIndex < focusableItems.length - 1) {
                    (focusableItems[currentIndex + 1] as HTMLElement).focus();
                } else {
                    (focusableItems[0] as HTMLElement).focus();
                }
                break;
            case 'ArrowUp':
                e.preventDefault();
                if (currentIndex > 0) {
                    (focusableItems[currentIndex - 1] as HTMLElement).focus();
                } else {
                    (focusableItems[focusableItems.length - 1] as HTMLElement).focus();
                }
                break;
            case 'Enter':
            case ' ':
                e.preventDefault();
                if (currentIndex >= 0) {
                    onSelect(currentIndex);
                    setIsOpen(false);
                    triggerRef.current?.focus();
                }
                break;
            case 'Escape':
                e.preventDefault();
                setIsOpen(false);
                triggerRef.current?.focus();
                break;
            case 'Tab':
                setIsOpen(false);
                break;
        }
    };

    // Focus first item when opened
    useEffect(() => {
        if (isOpen && listboxRef.current) {
            const firstItem = listboxRef.current.querySelector('[role="option"]') as HTMLElement;
            if (firstItem) {
                // setTimeout to allow rendering
                setTimeout(() => firstItem.focus(), 0);
            }
        }
    }, [isOpen]);

    return { listboxRef, triggerRef, handleTriggerKeyDown, handleListboxKeyDown };
}
