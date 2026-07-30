ALTER TABLE goal_templates
  DROP COLUMN IF EXISTS is_custom,
  DROP COLUMN IF EXISTS created_by,
  DROP COLUMN IF EXISTS created_at,
  DROP COLUMN IF EXISTS updated_at;
