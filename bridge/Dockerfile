# Headless Proton Mail Bridge, pinned to the same version as the desktop app.
# The .deb is repacked to drop GUI dependencies (Qt/GL) it never uses here.
FROM debian:bookworm-slim

ARG BRIDGE_VERSION=3.26.0

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends pass ca-certificates libsecret-1-0 libgl1 libfido2-1 curl binutils; \
    mkdir /deb && cd /deb; \
    curl -fsSLo bridge.deb "https://proton.me/download/bridge/protonmail-bridge_${BRIDGE_VERSION}-1_amd64.deb"; \
    ar x bridge.deb; \
    mkdir control && tar xf control.tar.* -C control; \
    sed -i 's/^Depends: .*$/Depends: libc6, libsecret-1-0, libstdc++6, libgcc-s1, libgl1, libfido2-1/' control/control; \
    (cd control && tar czf ../control.tar.gz .); \
    rm -f control.tar.xz control.tar.zst; \
    ar rcs repacked.deb debian-binary control.tar.gz data.tar.*; \
    apt-get install -y --no-install-recommends ./repacked.deb; \
    apt-get purge -y binutils curl; apt-get autoremove -y; \
    ! ldd /usr/lib/protonmail/bridge/bridge | grep 'not found'; \
    rm -rf /var/lib/apt/lists/* /deb

COPY entrypoint.sh /protonmail/entrypoint.sh
COPY gpgparams /protonmail/gpgparams
RUN chmod +x /protonmail/entrypoint.sh

ENTRYPOINT ["/protonmail/entrypoint.sh"]
