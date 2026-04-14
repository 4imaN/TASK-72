-- seeds/001_bootstrap.sql
-- Bootstraps roles, permissions, job families, and one account per role.
-- Passwords are set to BOOTSTRAP_PLACEHOLDER and replaced by init_db.sh
-- using bcrypt hashes generated from the runtime secret files.

-- ─────────────────────────────────────────────────────────────────────────────
-- Roles
-- ─────────────────────────────────────────────────────────────────────────────

INSERT INTO roles (name, display_name) VALUES
  ('learner',       'Learner'),
  ('procurement',   'Procurement Specialist'),
  ('approver',      'Approver'),
  ('finance',       'Finance Analyst'),
  ('moderator',     'Content Moderator'),
  ('admin',         'System Administrator')
ON CONFLICT (name) DO NOTHING;

-- ─────────────────────────────────────────────────────────────────────────────
-- Permissions
-- ─────────────────────────────────────────────────────────────────────────────

INSERT INTO permissions (code, description) VALUES
  -- Catalog
  ('catalog:read',              'Browse and search the learning library'),
  ('catalog:write',             'Create and edit catalog resources'),
  ('catalog:publish',           'Publish and archive resources'),
  -- Taxonomy
  ('taxonomy:read',             'View taxonomy hierarchy and synonyms'),
  ('taxonomy:write',            'Edit taxonomy, synonyms, and conflicts'),
  -- Learning
  ('learning:enroll',           'Enroll in learning paths'),
  ('learning:progress',         'Track and update own learning progress'),
  ('learning:export_own',       'Export own learning records as CSV'),
  ('learning:export_any',       'Export any user''s learning records (admin)'),
  -- Procurement
  ('orders:read',               'View vendor orders'),
  ('orders:write',              'Create and update vendor orders'),
  ('orders:approve',            'Approve or reject vendor orders (segregation of duties)'),
  ('reviews:write',             'Submit and manage reviews'),
  ('merchant_replies:write',    'Record merchant correspondence'),
  -- Disputes
  ('appeals:write',             'Submit dispute appeals'),
  ('appeals:decide',            'Decide arbitration outcomes'),
  -- Moderation
  ('moderation:write',          'Apply review visibility states'),
  -- Finance
  ('reconciliation:read',       'View reconciliation runs and variances'),
  ('reconciliation:write',      'Run reconciliation and manage variances'),
  ('writeoffs:approve',         'Approve write-off suggestions'),
  ('settlements:write',         'Create and advance settlement batches'),
  ('ar_ap:write',               'Generate AR/AP entries'),
  -- Exports
  ('exports:write',             'Trigger and manage export jobs'),
  -- Configuration
  ('config:read',               'View configuration center'),
  ('config:write',              'Edit feature flags and parameters'),
  ('users:read',                'View user list (admin)'),
  ('users:write',               'Create and manage users (admin)'),
  ('audit:read',                'View audit logs'),
  ('sensitive_data:reveal',     'Reveal masked personal data (admin only)')
ON CONFLICT (code) DO NOTHING;

-- ─────────────────────────────────────────────────────────────────────────────
-- Role → Permission assignments
-- ─────────────────────────────────────────────────────────────────────────────

-- Helper: assign permissions to a role by code
DO $$
DECLARE
  v_role_id     BIGINT;
  v_perm_id     BIGINT;
  v_role_name   TEXT;
  v_perm_codes  TEXT[];
  v_code        TEXT;
BEGIN
  -- Learner
  v_role_name  := 'learner';
  v_perm_codes := ARRAY['catalog:read','taxonomy:read','learning:enroll','learning:progress','learning:export_own'];
  SELECT id INTO v_role_id FROM roles WHERE name = v_role_name;
  FOREACH v_code IN ARRAY v_perm_codes LOOP
    SELECT id INTO v_perm_id FROM permissions WHERE code = v_code;
    INSERT INTO role_permissions(role_id, permission_id) VALUES (v_role_id, v_perm_id) ON CONFLICT DO NOTHING;
  END LOOP;

  -- Procurement Specialist
  v_role_name  := 'procurement';
  v_perm_codes := ARRAY['catalog:read','taxonomy:read','orders:read','orders:write','reviews:write','merchant_replies:write','appeals:write'];
  SELECT id INTO v_role_id FROM roles WHERE name = v_role_name;
  FOREACH v_code IN ARRAY v_perm_codes LOOP
    SELECT id INTO v_perm_id FROM permissions WHERE code = v_code;
    INSERT INTO role_permissions(role_id, permission_id) VALUES (v_role_id, v_perm_id) ON CONFLICT DO NOTHING;
  END LOOP;

  -- Approver
  v_role_name  := 'approver';
  v_perm_codes := ARRAY['catalog:read','orders:read','orders:approve','reviews:write','appeals:decide','reconciliation:read'];
  SELECT id INTO v_role_id FROM roles WHERE name = v_role_name;
  FOREACH v_code IN ARRAY v_perm_codes LOOP
    SELECT id INTO v_perm_id FROM permissions WHERE code = v_code;
    INSERT INTO role_permissions(role_id, permission_id) VALUES (v_role_id, v_perm_id) ON CONFLICT DO NOTHING;
  END LOOP;

  -- Finance Analyst
  v_role_name  := 'finance';
  v_perm_codes := ARRAY['catalog:read','orders:read','reconciliation:read','reconciliation:write','writeoffs:approve','settlements:write','ar_ap:write','exports:write','appeals:decide'];
  SELECT id INTO v_role_id FROM roles WHERE name = v_role_name;
  FOREACH v_code IN ARRAY v_perm_codes LOOP
    SELECT id INTO v_perm_id FROM permissions WHERE code = v_code;
    INSERT INTO role_permissions(role_id, permission_id) VALUES (v_role_id, v_perm_id) ON CONFLICT DO NOTHING;
  END LOOP;

  -- Content Moderator
  v_role_name  := 'moderator';
  v_perm_codes := ARRAY['catalog:read','catalog:write','catalog:publish','taxonomy:read','taxonomy:write','reviews:write','moderation:write'];
  SELECT id INTO v_role_id FROM roles WHERE name = v_role_name;
  FOREACH v_code IN ARRAY v_perm_codes LOOP
    SELECT id INTO v_perm_id FROM permissions WHERE code = v_code;
    INSERT INTO role_permissions(role_id, permission_id) VALUES (v_role_id, v_perm_id) ON CONFLICT DO NOTHING;
  END LOOP;

  -- System Administrator (all permissions)
  v_role_name  := 'admin';
  SELECT id INTO v_role_id FROM roles WHERE name = v_role_name;
  FOR v_perm_id IN SELECT id FROM permissions LOOP
    INSERT INTO role_permissions(role_id, permission_id) VALUES (v_role_id, v_perm_id) ON CONFLICT DO NOTHING;
  END LOOP;
END $$;

-- ─────────────────────────────────────────────────────────────────────────────
-- Bootstrap accounts (one per role)
-- Passwords are BOOTSTRAP_PLACEHOLDER — replaced on first init_db run
-- ─────────────────────────────────────────────────────────────────────────────

INSERT INTO users (id, username, email, display_name, password_hash, force_password_reset)
VALUES
  (gen_random_uuid(), 'bootstrap_learner',      'learner@portal.local',      'Bootstrap Learner',      'BOOTSTRAP_PLACEHOLDER', TRUE),
  (gen_random_uuid(), 'bootstrap_procurement',   'procurement@portal.local',  'Bootstrap Procurement',  'BOOTSTRAP_PLACEHOLDER', TRUE),
  (gen_random_uuid(), 'bootstrap_approver',      'approver@portal.local',     'Bootstrap Approver',     'BOOTSTRAP_PLACEHOLDER', TRUE),
  (gen_random_uuid(), 'bootstrap_finance',       'finance@portal.local',      'Bootstrap Finance',      'BOOTSTRAP_PLACEHOLDER', TRUE),
  (gen_random_uuid(), 'bootstrap_moderator',     'moderator@portal.local',    'Bootstrap Moderator',    'BOOTSTRAP_PLACEHOLDER', TRUE),
  (gen_random_uuid(), 'bootstrap_admin',         'admin@portal.local',        'Bootstrap Admin',        'BOOTSTRAP_PLACEHOLDER', TRUE)
ON CONFLICT (username) DO NOTHING;

-- Assign roles to bootstrap accounts
DO $$
DECLARE
  v_user_id   UUID;
  v_role_id   BIGINT;
BEGIN
  SELECT id INTO v_user_id FROM users WHERE username = 'bootstrap_learner';
  SELECT id INTO v_role_id FROM roles WHERE name = 'learner';
  IF v_user_id IS NOT NULL AND v_role_id IS NOT NULL THEN
    INSERT INTO user_roles(user_id, role_id) VALUES (v_user_id, v_role_id) ON CONFLICT DO NOTHING;
  END IF;

  SELECT id INTO v_user_id FROM users WHERE username = 'bootstrap_procurement';
  SELECT id INTO v_role_id FROM roles WHERE name = 'procurement';
  IF v_user_id IS NOT NULL AND v_role_id IS NOT NULL THEN
    INSERT INTO user_roles(user_id, role_id) VALUES (v_user_id, v_role_id) ON CONFLICT DO NOTHING;
  END IF;

  SELECT id INTO v_user_id FROM users WHERE username = 'bootstrap_approver';
  SELECT id INTO v_role_id FROM roles WHERE name = 'approver';
  IF v_user_id IS NOT NULL AND v_role_id IS NOT NULL THEN
    INSERT INTO user_roles(user_id, role_id) VALUES (v_user_id, v_role_id) ON CONFLICT DO NOTHING;
  END IF;

  SELECT id INTO v_user_id FROM users WHERE username = 'bootstrap_finance';
  SELECT id INTO v_role_id FROM roles WHERE name = 'finance';
  IF v_user_id IS NOT NULL AND v_role_id IS NOT NULL THEN
    INSERT INTO user_roles(user_id, role_id) VALUES (v_user_id, v_role_id) ON CONFLICT DO NOTHING;
  END IF;

  SELECT id INTO v_user_id FROM users WHERE username = 'bootstrap_moderator';
  SELECT id INTO v_role_id FROM roles WHERE name = 'moderator';
  IF v_user_id IS NOT NULL AND v_role_id IS NOT NULL THEN
    INSERT INTO user_roles(user_id, role_id) VALUES (v_user_id, v_role_id) ON CONFLICT DO NOTHING;
  END IF;

  SELECT id INTO v_user_id FROM users WHERE username = 'bootstrap_admin';
  SELECT id INTO v_role_id FROM roles WHERE name = 'admin';
  IF v_user_id IS NOT NULL AND v_role_id IS NOT NULL THEN
    INSERT INTO user_roles(user_id, role_id) VALUES (v_user_id, v_role_id) ON CONFLICT DO NOTHING;
  END IF;
END $$;

-- ─────────────────────────────────────────────────────────────────────────────
-- Job families
-- ─────────────────────────────────────────────────────────────────────────────

INSERT INTO job_families (code, name, description) VALUES
  ('ENG',  'Engineering',       'Software and systems engineering roles'),
  ('OPS',  'Operations',        'Operational and supply chain roles'),
  ('FIN',  'Finance',           'Finance, accounting, and procurement roles'),
  ('HR',   'Human Resources',   'People operations and talent roles'),
  ('SALE', 'Sales',             'Sales and business development roles'),
  ('PROD', 'Product',           'Product management and design roles'),
  ('DATA', 'Data & Analytics',  'Data science and analytics roles'),
  ('GEN',  'General',           'General workforce roles')
ON CONFLICT (code) DO NOTHING;

-- ─────────────────────────────────────────────────────────────────────────────
-- Root skill tags
-- ─────────────────────────────────────────────────────────────────────────────

INSERT INTO skill_tags (code, canonical_name) VALUES
  ('leadership',      'Leadership'),
  ('communication',   'Communication'),
  ('project_mgmt',    'Project Management'),
  ('data_analysis',   'Data Analysis'),
  ('procurement',     'Procurement'),
  ('finance',         'Finance'),
  ('compliance',      'Compliance'),
  ('software_dev',    'Software Development'),
  ('ops',             'Operations')
ON CONFLICT (code) DO NOTHING;

-- ─────────────────────────────────────────────────────────────────────────────
-- Configuration defaults
-- ─────────────────────────────────────────────────────────────────────────────

INSERT INTO config_parameters (param_key, param_value, value_type, description) VALUES
  ('session.idle_timeout_seconds',    '900',     'integer', '15-minute idle session timeout'),
  ('session.max_timeout_seconds',     '28800',   'integer', '8-hour absolute session timeout'),
  ('recommendation.diversity_cap_pct','40',      'integer', 'Max % from one category in recommendation carousel'),
  ('search.default_fuzzy_enabled',    'true',    'string',  'Enable trigram fuzzy matching by default'),
  ('search.default_synonym_enabled',  'true',    'string',  'Enable synonym expansion by default'),
  ('search.default_pinyin_enabled',   'false',   'string',  'Enable pinyin expansion by default'),
  ('writeoff.auto_suggest_threshold', '500',     'integer', 'Variance threshold in minor units for write-off suggestion'),
  ('mfa.required_for_roles',          '[]',      'json',    'JSON array of role names that require MFA'),
  ('export.file_drop_path',           '/app/storage/private/exports', 'string', 'Local file-drop outbox path'),
  ('client.version_grace_days',       '14',      'integer', '14-day read-only grace period for unsupported clients')
ON CONFLICT (param_key) DO NOTHING;

INSERT INTO config_flags (flag_key, flag_value, description) VALUES
  ('mfa.enabled',                TRUE,  'Enable MFA TOTP support globally'),
  ('search.synonym_expansion',   TRUE,  'Enable synonym expansion in search'),
  ('search.pinyin_expansion',    FALSE, 'Enable pinyin expansion in search'),
  ('recommendations.enabled',    TRUE,  'Enable recommendation carousels'),
  ('exports.webhook_enabled',    TRUE,  'Enable LAN webhook exports'),
  ('compatibility.check_enabled',TRUE,  'Enforce client version compatibility rules')
ON CONFLICT (flag_key) DO NOTHING;

-- ─────────────────────────────────────────────────────────────────────────────
-- Demo resources for first-boot usability
-- ─────────────────────────────────────────────────────────────────────────────

INSERT INTO resources (id, title, description, content_type, category, publish_date, is_published, created_at)
SELECT
  gen_random_uuid(),
  title,
  description,
  content_type,
  category,
  publish_date::DATE,
  TRUE,
  NOW()
FROM (VALUES
  ('Introduction to Leadership',      'Core leadership principles for new managers.',             'article', 'leadership',  '2024-01-15'),
  ('Procurement Best Practices',      'Vendor evaluation and order governance essentials.',        'article', 'procurement', '2024-02-01'),
  ('Data Analysis Fundamentals',      'Introduction to data-driven decision making.',              'course',  'data',        '2024-02-20'),
  ('Finance for Non-Finance Managers','Understanding P&L, budgets, and cost centres.',            'article', 'finance',     '2024-03-05'),
  ('Effective Communication Skills',  'Written and verbal communication in the workplace.',       'video',   'communication','2024-03-12'),
  ('Project Management Basics',       'Agile and waterfall methodology overview.',                'course',  'project_mgmt','2024-04-01'),
  ('Compliance & Ethics',             'Regulatory compliance fundamentals for all employees.',    'article', 'compliance',  '2024-04-15'),
  ('Software Development Lifecycle',  'SDLC from requirements to deployment.',                    'course',  'engineering', '2024-05-01')
) AS t(title, description, content_type, category, publish_date)
WHERE NOT EXISTS (SELECT 1 FROM resources WHERE title = t.title);

-- Demo learning path
DO $$
DECLARE
  v_path_id       UUID;
  v_resource_id   UUID;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM learning_paths WHERE title = 'New Manager Foundations') THEN
    INSERT INTO learning_paths (id, title, description, is_published)
    VALUES (gen_random_uuid(), 'New Manager Foundations',
            'Essential skills for first-time managers including leadership, communication, and finance basics.',
            TRUE)
    RETURNING id INTO v_path_id;

    -- Required items
    FOR v_resource_id IN
      SELECT id FROM resources WHERE title IN (
        'Introduction to Leadership', 'Effective Communication Skills', 'Finance for Non-Finance Managers'
      )
    LOOP
      INSERT INTO learning_path_items (path_id, resource_id, item_type, sort_order)
      VALUES (v_path_id, v_resource_id, 'required', 1);
    END LOOP;

    -- Elective items
    FOR v_resource_id IN
      SELECT id FROM resources WHERE title IN (
        'Data Analysis Fundamentals', 'Project Management Basics', 'Compliance & Ethics'
      )
    LOOP
      INSERT INTO learning_path_items (path_id, resource_id, item_type, sort_order)
      VALUES (v_path_id, v_resource_id, 'elective', 2);
    END LOOP;

    -- Rule: 3 required + any 1 elective
    INSERT INTO learning_path_rules (path_id, required_count, elective_minimum, completion_description)
    VALUES (v_path_id, 3, 1, 'Complete all 3 required modules plus at least 1 elective.');
  END IF;
END $$;
