-- Reverse of the rule binding, rollout and coverage tables.

DROP POLICY IF EXISTS tenant_isolation_rule_coverage_unsupported ON rule_coverage_unsupported;
DROP INDEX IF EXISTS idx_rule_coverage_unsupported_organization_id_rule_id;
DROP INDEX IF EXISTS idx_rule_coverage_unsupported_tenant_id_organization_id;
DROP TABLE IF EXISTS rule_coverage_unsupported;

DROP POLICY IF EXISTS tenant_isolation_rule_rollout ON rule_rollout;
DROP INDEX IF EXISTS idx_rule_rollout_tenant_id_organization_id;
DROP TABLE IF EXISTS rule_rollout;

DROP POLICY IF EXISTS tenant_isolation_rule_bindings ON rule_bindings;
DROP INDEX IF EXISTS idx_rule_bindings_organization_id_rule_id;
DROP INDEX IF EXISTS idx_rule_bindings_tenant_id_organization_id;
DROP INDEX IF EXISTS uq_rule_bindings_selector_precedence;
DROP TABLE IF EXISTS rule_bindings;

-- Both are only reachable through the tables above, so they go last.
DROP INDEX IF EXISTS organizations_tenant_id_id_key;
DROP FUNCTION IF EXISTS app_tenant_visible(UUID);
