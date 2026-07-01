package device

const logCountSQL = `SELECT COUNT(*) FROM device_logs
WHERE org_id = current_setting('app.current_org')::uuid
  AND device_id = $1
  AND ($2 = '' OR (CASE level
        WHEN 'TRACE' THEN 0
        WHEN 'DEBUG' THEN 1
        WHEN 'INFO'  THEN 2
        WHEN 'WARN'  THEN 3
        WHEN 'ERROR' THEN 4
        ELSE -1
      END) >= (CASE $2
        WHEN 'TRACE' THEN 0
        WHEN 'DEBUG' THEN 1
        WHEN 'INFO'  THEN 2
        WHEN 'WARN'  THEN 3
        WHEN 'ERROR' THEN 4
        ELSE -1
      END))
  AND ($3 = '' OR timestamp >= $3)
  AND ($4 = '' OR timestamp <= $4)
  AND ($5 = '' OR message LIKE $6)`
