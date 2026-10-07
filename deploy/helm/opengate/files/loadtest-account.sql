-- Callers set the address and password as psql variables, so no credential reaches a command
-- line, where every process in the pod and the audit log could read it.

-- pgcrypto's 'bf' matches the server's bcrypt cost 10, so the server accepts the hash.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- The ids are the server's default tenant and Administrators group; users.is_admin mirrors
-- membership of that group.
INSERT INTO users (id, tenant_id, email, password_hash, display_name, is_admin)
VALUES (
  '00000000-0000-0000-0000-00000000000a',
  '00000000-0000-0000-0000-000000000002',
  :'email',
  crypt(:'account_password', gen_salt('bf', 10)),
  'Load-test service account',
  TRUE
)
ON CONFLICT (email) DO UPDATE
  SET password_hash = crypt(:'account_password', gen_salt('bf', 10)),
      is_admin      = TRUE;

INSERT INTO security_group_members (group_id, user_id, tenant_id)
SELECT '00000000-0000-0000-0000-000000000001', u.id, u.tenant_id
FROM users u
WHERE u.email = :'email'
ON CONFLICT DO NOTHING;
