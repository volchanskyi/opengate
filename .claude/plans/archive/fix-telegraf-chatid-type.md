# Plan: Fix Grafana Crash — chatid Type Mismatch

## Context

Grafana is in a crash loop (`Restarting (1)`) because provisioning fails on `contact-points.yml`. The error:

```
json: cannot unmarshal number into Go struct field Config.chatid of type string
```

`TELEGRAM_CHAT_ID` (e.g. `-1001234567890`) is substituted into the YAML unquoted. YAML parses it as an integer, but Grafana's Go struct expects `chatid` as a `string`.

## Root Cause

File: `deploy/grafana/provisioning/alerting/contact-points.yml:11`

```yaml
chatid: ${TELEGRAM_CHAT_ID}   # ← YAML parses as number → Grafana rejects
```

## Fix

Quote the value so YAML treats it as a string:

```yaml
chatid: "${TELEGRAM_CHAT_ID}"
```

## File to Modify

- `deploy/grafana/provisioning/alerting/contact-points.yml` — line 11, wrap `${TELEGRAM_CHAT_ID}` in double quotes

## Verification

1. Commit and push to `dev`
2. Wait for CI → auto-merge to `main` → CD deploys to VPS
3. Or hotfix on VPS: edit `/opt/opengate/grafana/provisioning/alerting/contact-points.yml`, then:
   ```bash
   docker compose --project-name opengate-monitoring \
     -f /opt/opengate/docker-compose.monitoring.yml \
     --env-file /opt/opengate/.env.monitoring \
     up -d --force-recreate grafana
   ```
4. Verify: `ssh -L 3000:localhost:3000 ubuntu@163.192.34.124` → `http://localhost:3000` → Grafana login
