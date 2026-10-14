# Playwright's browser server, for the specs whose browser sits behind the home router. The version
# is the suite's own @playwright/test, read from web/package-lock.json by e2e-stack-up.sh.
ARG PLAYWRIGHT_VERSION
FROM mcr.microsoft.com/playwright:v${PLAYWRIGHT_VERSION}-noble
ARG PLAYWRIGHT_VERSION

USER pwuser
WORKDIR /home/pwuser/server
RUN npm init -y >/dev/null && npm install --no-audit --no-fund "playwright@${PLAYWRIGHT_VERSION}"

EXPOSE 3000
HEALTHCHECK --interval=2s --timeout=2s --retries=30 \
  CMD ["node", "-e", "require('net').connect(3000, '127.0.0.1').on('connect', () => process.exit(0)).on('error', () => process.exit(1))"]
ENTRYPOINT ["npx", "--no-install", "playwright", "run-server", "--port", "3000", "--host", "0.0.0.0"]
