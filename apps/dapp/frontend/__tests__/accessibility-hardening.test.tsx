import { describe, it, expect, vi } from 'vitest';
import React from 'react';
import { render, screen } from '@testing-library/react';
import { NotificationsToaster } from '@/components/notifications-toaster';
import * as NotificationProviderModule from '@/components/notifications-provider';

describe('Accessibility & Performance Hardening Pass (#871)', () => {
    it('announces critical safety toasts via assertive live regions and normal toasts via polite live regions', () => {
        vi.spyOn(NotificationProviderModule, 'useNotifications').mockReturnValue({
            toasts: [
                {
                    id: '1',
                    title: 'Emergency Pause',
                    message: 'Vault has been paused for safety.',
                    priority: 'safety',
                    createdAt: Date.now(),
                    read: false,
                },
                {
                    id: '2',
                    title: 'Deposit Successful',
                    message: 'Received 100 USDC into vault.',
                    priority: 'info',
                    createdAt: Date.now(),
                    read: false,
                },
            ],
            unreadCount: 2,
            dismissToast: vi.fn(),
            markAllAsRead: vi.fn(),
            markAsRead: vi.fn(),
            clearAll: vi.fn(),
            preferences: {} as unknown as NotificationProviderModule.NotificationPreferences,
            updatePreferences: vi.fn(),
            notifications: [],
            isMuted: vi.fn().mockReturnValue(false),
        });

        render(<NotificationsToaster />);

        const alertElement = screen.getByRole('alert');
        expect(alertElement).toBeDefined();
        expect(alertElement.getAttribute('aria-live')).toBe('assertive');
        expect(alertElement.getAttribute('aria-atomic')).toBe('true');

        const statusElement = screen.getByRole('status');
        expect(statusElement).toBeDefined();
        expect(statusElement.getAttribute('aria-live')).toBe('polite');
    });

    it('provides accessible names on dismiss icon buttons', () => {
        vi.spyOn(NotificationProviderModule, 'useNotifications').mockReturnValue({
            toasts: [
                {
                    id: '1',
                    title: 'Yield Harvested',
                    message: '10 USDC harvested.',
                    priority: 'info',
                    createdAt: Date.now(),
                    read: false,
                },
            ],
            unreadCount: 1,
            dismissToast: vi.fn(),
            markAllAsRead: vi.fn(),
            markAsRead: vi.fn(),
            clearAll: vi.fn(),
            preferences: {} as unknown as NotificationProviderModule.NotificationPreferences,
            updatePreferences: vi.fn(),
            notifications: [],
            isMuted: vi.fn().mockReturnValue(false),
        });

        render(<NotificationsToaster />);
        const dismissBtn = screen.getByRole('button', { name: /dismiss yield harvested/i });
        expect(dismissBtn).toBeDefined();
        expect(dismissBtn.getAttribute('aria-label')).toBe('Dismiss Yield Harvested');
    });
});
