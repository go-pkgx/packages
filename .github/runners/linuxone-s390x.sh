#!/usr/bin/env bash
# Provision a self-hosted GitHub Actions runner on a LinuxONE (s390x) VM, for
# the `linux/s390x` lane build.yml gained in #243.
#
# Run this ON THE VM, not here.
#
# # Why a script rather than a paragraph
#
# The runner has to be BUILT: actions/runner does not ship an s390x binary. It
# is .NET, and linux-s390x is a community-supported target with no Microsoft
# builds (actions/runner#2263). IBM/action-runner-image-pz carries the dotnet
# environment and the IBM Z patches. That is several steps with an interactive
# menu in the middle, a step nobody should improvise (the registration token),
# and three facts about the result that are not written down anywhere — see
# "What the built runner does not tell you" below.
#
# # The token never reaches a command line
#
# `config.sh --token <T>` is the documented form and it is the wrong one here:
# an argument is visible in `ps`, in the shell history, and in any log that
# echoes the command. A token has been lost to exactly that on this project's
# machines, twice. config.sh PROMPTS for the token when --token is absent, so
# this script omits it and lets the value be typed or piped:
#
#     ./linuxone-s390x.sh                 # type the token at the prompt
#     ./linuxone-s390x.sh < token.txt     # or feed it on stdin
#
# Get one from  Settings -> Actions -> Runners -> New self-hosted runner  on
# github.com/go-pkgx/packages. It expires in an hour, which is why it is not
# stored anywhere.
#
# # What it does NOT do
#
# It does not install docker or podman. The lane runs with `container: ""` —
# the VM IS the build environment, like the Incus pool's own entries — and the
# job's `base tools` step uses apt-get, so this assumes an Ubuntu image
# (gaplib covers 22.04 and 24.04).
#
# The host is the project's own LinuxONE Community Cloud VM, dedicated rather
# than shared. Its address and account are deliberately NOT written here: this
# repository is public, and a hostname plus a username is two thirds of an
# attempt. Whoever runs this script is already on the machine.
#
# Another repository in the fleet reaches the same VM a different way: an
# ubuntu-latest job that SSHes in with a key from a repo secret. That is right
# for what it does — `go vet && go test` is one self-contained command on a
# public repo — and wrong here. `bk factory` checks out a workspace, runs
# inside a container, uploads artefacts and PUSHES TO GHCR with a token;
# reproducing that over SSH means rewriting the lane and shipping a registry
# credential to the VM. The job body is built to run where the work happens, so
# the runner goes where the work happens.
#
# # What the built runner does not tell you
#
# All three of these were found by hitting them, on a runner that had
# registered and gone online and still could not run a job.
#
# 1. THE PREBUILT TARBALL IS TOO OLD. gaplib publishes a runner package built
#    in 2024 (3.314.1). `actions/checkout@v7` declares `using: node24`, and
#    that runner answers
#
#        Unsupported runtime 'node24' ... supported: node16, node20
#
#    The accepted list is COMPILED INTO the binary. Unpacking a node24 into
#    `externals/` changes nothing — this was tried, and it did not help. The
#    runner has to be rebuilt from a recent actions/runner tag, which is what
#    RUNNER_REF below does.
#
# 2. runsvc.sh DEFAULTS TO node16, which the rebuilt package no longer ships:
#
#        ./externals/node16/bin/node: No such file or directory   (status=127)
#
#    It honours GITHUB_ACTIONS_RUNNER_FORCED_NODE_VERSION. Setting that in
#    `.env` does NOT work: the systemd unit svc.sh installs has no
#    EnvironmentFile, so nothing in `.env` reaches runsvc.sh.
#
# 3. THE PACKAGE IS FRAMEWORK-DEPENDENT. There is no self-contained s390x
#    dotnet runtime to bundle, so the runner uses the system one gaplib's
#    setup installs, and needs DOTNET_ROOT to find it.
#
# (2) and (3) are both environment for a service, so both go in a systemd
# drop-in rather than in files the next `svc.sh install` would rewrite.
set -euo pipefail

REPO="${REPO:-https://github.com/go-pkgx/packages}"
LABELS="${LABELS:-self-hosted,linux,s390x}"
NAME="${NAME:-linuxone-s390x}"  # what is registered today; build.yml selects on the LABELS, not this
WORKDIR="${WORKDIR:-$HOME/actions-runner}"
# Pinned rather than a moving main: a runner you rebuild from a branch is a
# runner whose behaviour changes without a commit here saying so.
RUNNER_REF="${RUNNER_REF:-v2.337.0}"
GAPLIB_REF="${GAPLIB_REF:-main}"

echo "== arch check"
arch="$(uname -m)"
[ "$arch" = "s390x" ] || { echo "this is $arch, not s390x — wrong machine" >&2; exit 1; }

echo "== prerequisites"
sudo apt-get update -qq
sudo apt-get install -y --no-install-recommends \
  git curl ca-certificates jq build-essential

echo "== gaplib: the dotnet environment and the IBM Z patches"
# run.sh is INTERACTIVE. Choose: environment = VM, OS = your Ubuntu version,
# setup = Complete. It installs a dotnet SDK for s390x and leaves the runner
# patch in patches/. Its own prebuilt runner tarball is too old to use — see
# (1) above — so this script rebuilds from RUNNER_REF instead.
rm -rf "$HOME/gaplib"
git clone --depth 1 --branch "$GAPLIB_REF" \
  https://github.com/IBM/action-runner-image-pz "$HOME/gaplib"
( cd "$HOME/gaplib" && bash run.sh )

echo "== build actions/runner $RUNNER_REF for s390x"
rm -rf "$HOME/runner-src"
git clone --depth 1 --branch "$RUNNER_REF" \
  https://github.com/actions/runner "$HOME/runner-src"
cd "$HOME/runner-src"
git apply "$HOME/gaplib/patches/runner-sdk8-s390x.patch"
# The tree pins an SDK version gaplib does not install; take the one that is
# there. Tests are skipped: gaplib documents them as failing on s390x, and
# `dev.sh package` does not run them.
sed -i 's/"version": "[^"]*"/"version": "8.0.100"/' src/global.json
cd src
./dev.sh layout Release
./dev.sh package Release
pkg="$(ls -1 ../_package/actions-runner-linux-s390x-*.tar.gz | tail -1)"
echo "built $pkg"

echo "== register"
# --token is deliberately ABSENT: config.sh prompts, and a prompt keeps the
# secret off argv. --unattended is likewise absent, because it would require
# the token as a flag.
mkdir -p "$WORKDIR"
cd "$WORKDIR"
tar xzf "$pkg"
./config.sh --url "$REPO" --labels "$LABELS" --name "$NAME"

echo "== install the service, then give it the two things it cannot find"
sudo ./svc.sh install
unit="actions.runner.$(echo "$REPO" | sed 's#.*/\([^/]*\)/\([^/]*\)$#\1-\2#').$NAME.service"
sudo mkdir -p "/etc/systemd/system/$unit.d"
sudo tee "/etc/systemd/system/$unit.d/override.conf" >/dev/null <<CONF
# See "What the built runner does not tell you", points (2) and (3), in
# .github/runners/linuxone-s390x.sh. A drop-in rather than .env or a patched
# runsvc.sh: the unit reads no EnvironmentFile, and svc.sh rewrites its own
# files on the next install.
[Service]
Environment=GITHUB_ACTIONS_RUNNER_FORCED_NODE_VERSION=node20
Environment=DOTNET_ROOT=/usr/lib/dotnet
CONF
sudo systemctl daemon-reload
sudo ./svc.sh start
sleep 5
systemctl is-active "$unit"

echo
echo "== verify, from anywhere"
echo "    gh api repos/go-pkgx/packages/actions/runners -q '.runners[].name'"
echo
echo "== first build, once it answers"
echo "    gh workflow run build --repo go-pkgx/packages \\"
echo "      -f arch=s390x -f sovereign=0 -f recipes='gnu.org/make'"
echo
echo "sovereign=0 on purpose: the FROM-scratch rootfs needs glibc, llvm and"
echo "linux-headers as s390x bottles and there are none yet, so the first pass"
echo "builds on this VM's own distribution and the second can switch."
