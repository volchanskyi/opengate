# A simulated router for the browser test stack. It translates the addresses of the network behind
# it to its own and passes back only replies; file capabilities let it run unprivileged.
FROM alpine:3.21.3

# iproute2's ip drops every capability its caller cannot pass on, so setpriv hands it NET_ADMIN as
# an inheritable and ambient capability.
RUN apk add --no-cache bash=5.2.37-r0 iptables=1.8.11-r1 iproute2=6.11.0-r0 iproute2-tc=6.11.0-r0 \
      setpriv=2.40.4-r1 \
    && apk add --no-cache --virtual .caps libcap-utils=2.78-r0 \
    && setcap cap_net_admin,cap_net_raw+ep /usr/sbin/xtables-nft-multi \
    && setcap cap_net_admin+ep /usr/bin/setpriv \
    && setcap cap_net_admin+ep /sbin/tc \
    && apk del .caps \
    && addgroup -S router && adduser -S router -G router

COPY router.sh /usr/local/bin/router.sh

USER router
HEALTHCHECK --interval=2s --timeout=2s --retries=15 CMD ["test", "-f", "/tmp/router-ready"]
ENTRYPOINT ["/usr/local/bin/router.sh"]
CMD ["start"]
