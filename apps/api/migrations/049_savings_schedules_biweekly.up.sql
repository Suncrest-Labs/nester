ALTER TABLE savings_schedules DROP CONSTRAINT IF EXISTS savings_schedules_frequency_check;
ALTER TABLE savings_schedules ADD CONSTRAINT savings_schedules_frequency_check
    CHECK (frequency IN ('weekly', 'biweekly', 'monthly'));
