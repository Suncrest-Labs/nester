CREATE TABLE goal_templates (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             VARCHAR(100) NOT NULL,
    description      TEXT NOT NULL,
    category         VARCHAR(50)  NOT NULL,
    suggested_amount NUMERIC(20,8) NOT NULL,
    currency         VARCHAR(10)  NOT NULL DEFAULT 'USDC',
    suggested_months INT NOT NULL,
    icon             VARCHAR(50)  NOT NULL
);

INSERT INTO goal_templates (name, description, category, suggested_amount, currency, suggested_months, icon) VALUES
('Emergency Fund', 'Build a 3-month safety net for unexpected expenses.', 'emergency_fund', 3000, 'USDC', 6, 'shield-check'),
('Vacation', 'Save up for your dream getaway without the financial stress.', 'travel', 1500, 'USDC', 12, 'plane'),
('New Device', 'Upgrade your phone, laptop, or tablet.', 'other', 800, 'USDC', 3, 'smartphone'),
('Home Down Payment', 'Start the journey toward owning your own place.', 'housing', 10000, 'USDC', 24, 'home'),
('School Fees', 'Prepare for upcoming tuition and educational expenses.', 'education', 2000, 'USDC', 9, 'graduation-cap');
