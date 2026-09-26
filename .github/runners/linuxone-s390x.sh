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
# menu in the middle, and a step nobody should improvise: the registration
# token.
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
# public repo — and wrong here. `bk factory`
# checks out a workspace, runs inside a container, uploads artefacts and PUSHES
# TO GHCR with a token; reproducing that over SSH means rewriting the lane and
# shipping a registry credential to the VM. The job body is built to run where
# the work happens, so the runner goes where the work happens.
#
# It has not been run by its author: a script written for a machine one cannot
# reach is a proposal until somebody executes it. Read it before you do.
set -euo pipefail

REPO="${REPO:-https://github.com/go-pkgx/packages}"
LABELS="${LABELS:-self-hosted,linux,s390x}"
NAME="${NAME:-linuxone-$(hostname -s)}"
WORKDIR="${WORKDIR:-$HOME/actions-runner}"

echo "== arch check"
arch="$(uname -m)"
[ "$arch" = "s390x" ] || { echo "this is $arch, not s390x — wrong machine" >&2; exit 1; }

echo "== prerequisites"
sudo apt-get update -qq
sudo apt-get install -y --no-install-recommends \
  git curl ca-certificates jq build-essential

echo "== build the runner for s390x (IBM/action-runner-image-pz)"
# The upstream that exists BECAUSE GitHub does not ship this binary. Pinned to
# a tag rather than a moving main: a runner you rebuild from a branch is a
# runner whose behaviour changes without a commit here saying so.
GAPLIB_REF="${GAPLIB_REF:-main}"
rm -rf "$HOME/action-runner-image-pz"
git clone --depth 1 --branch "$GAPLIB_REF" \
  https://github.com/IBM/action-runner-image-pz "$HOME/action-runner-image-pz"
cd "$HOME/action-runner-image-pz"
echo
echo "run.sh is INTERACTIVE. Choose: environment = VM, OS = your Ubuntu"
echo "version, setup = Complete. It builds actions/runner for s390x and"
echo "leaves a tarball; note where it puts it."
echo
bash run.sh

echo "== register"
# --token is deliberately ABSENT: config.sh prompts, and a prompt keeps the
# secret off argv. --unattended is likewise absent, because it would require
# the token as a flag.
mkdir -p "$WORKDIR"
cd "$WORKDIR"
echo "unpack the tarball run.sh produced into $WORKDIR, then:"
echo
echo "    ./config.sh --url $REPO --labels $LABELS --name $NAME"
echo
echo "It will ask for the registration token. Paste it; do not pass it as an"
echo "argument."
echo
echo "== then run it as a service"
echo "    sudo ./svc.sh install && sudo ./svc.sh start"
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
