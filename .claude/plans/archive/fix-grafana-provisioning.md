# Fix Grafana Provisioning — Contact Points, Datasource UIDs, Notification Policies

## Context

Grafana is now running on the VPS (port 3000 accessible via SSH tunnel) but with provisioning stripped down to get it started. Three issues need fixing in the repo:

1. **Telegram chatid type coercion** — Known Grafana bug (#69950). Env var `${TELEGRAM_CHAT_ID}` expands to a number, but the Go struct expects a string. Workaround: YAML literal block scalar (`|-`) preserves string type.
2. **Alert rules: "data source not found"** — Rules reference `datasourceUid: VictoriaMetrics` but the datasource has no explicit `uid` set, so Grafana auto-generates one. Fix: add explicit `uid` to datasource definitions.
3. **Email contact point validation** — Empty `addresses: ""` fails Grafana 11.x strict validation. Fix: remove the placeholder email contact point.

## Files to Modify

### 1. `deploy/grafana/provisioning/datasources/datasources.yml`

Add explicit `uid` fields matching what alert rules expect:

```yaml
datasources:
  - uid: VictoriaMetrics
    name: VictoriaMetrics
    type: prometheus
    access: proxy
    url: http://opengate-victoriametrics:8428
    isDefault: true
    editable: false

  - uid: Loki
    name: Loki
    type: loki
    access: proxy
    url: http://opengate-loki:3100
    editable: false
    jsonData:
      derivedFields:
        - datasourceUid: VictoriaMetrics
          matcherRegex: '"trace_id":"(\\w+)"'
          name: TraceID
          url: ""
```

### 2. `deploy/grafana/provisioning/alerting/contact-points.yml`

Use YAML literal block scalar (`|-`) for chatid to work around Grafana bug #69950. Remove broken email contact point:

```yaml
apiVersion: 1

contactPoints:
  - orgId: 1
    name: telegram
    receivers:
      - uid: telegram-bot
        type: telegram
        settings:
          bottoken: ${TELEGRAM_BOT_TOKEN}
          chatid: |-
            ${TELEGRAM_CHAT_ID}
          parse_mode: HTML
        disableResolveMessage: false
```

### 3. `deploy/grafana/provisioning/alerting/notification-policies.yml`

No changes needed — already references `telegram` receiver which will exist.

### 4. `deploy/docker-compose.monitoring.yml` (line 2)

Update header comment to include `--env-file`:

```yaml
# Deployed with: docker compose --project-name opengate-monitoring -f docker-compose.monitoring.yml --env-file .env.monitoring up -d
```

## Verification

1. Commit and push to `dev`
2. Hotfix on VPS — write all 3 updated files, then:
   ```bash
   cd /opt/opengate && docker compose --project-name opengate-monitoring \
     -f docker-compose.monitoring.yml --env-file .env.monitoring \
     up -d --force-recreate grafana
   sleep 5 && docker logs opengate-grafana --tail 20
   ```
3. Confirm in Grafana UI:
   - Alerting > Contact points: Telegram contact point visible, "Test" button sends message
   - Alerting > Alert rules: All 6 rules show status (not "data source not found")
   - Data Sources: VictoriaMetrics and Loki connected
