# Base toolchain for the full desktop/headless workspace.
#
# Go, Node and the GitHub CLI are installed as devcontainer features. This
# image supplies the OS pieces those features do not: Python 3.12 with the
# system PyGObject bindings (the manager uses native GTK 4), an X server for
# GUI tests, and grim for compositor capture experiments.
FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        curl \
        dbus-x11 \
        git \
        gir1.2-gtk-4.0 \
        grim \
        libglib2.0-bin \
        libgtk-4-1 \
        pkg-config \
        python3 \
        python3-gi \
        python3-pip \
        python3-venv \
        xvfb \
    && rm -rf /var/lib/apt/lists/*

# The workspace venv is created with --system-site-packages so `import gi`
# resolves to Ubuntu's python3-gi rather than a separately built PyGObject.
