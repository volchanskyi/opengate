-- The load-test administrator, seeded from two places.
--
-- The chart's post-upgrade hook runs this on every release, and the staging
-- deploy runs it again once the browser suite is finished — that suite's
-- database reset truncates users, and the nightly load run has nobody to mint
-- an enrolment token against without this account.
--
-- Both callers set the address and the password as psql variables before
-- sending this file, so neither credential appears on a command line: a psql
-- argv is readable by every process in the Postgres pod and is recorded
-- verbatim in the API server's audit entry for the exec subresource.

-- The server hashes with bcrypt at cost 10, and pgcrypto's 'bf' produces
-- exactly that, so the hash written here is one the server's own comparison
-- accepts. Hashing inside the database keeps the plaintext off every command
-- line in this pod.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- The well-known ids the server itself uses: the default tenant and the
-- Administrators group. Membership of that group is what makes an account an
-- administrator; users.is_admin mirrors it.
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
