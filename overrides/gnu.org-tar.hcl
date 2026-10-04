project = "gnu.org/tar"
why     = "tar auto-detects the builder's SELinux and links a library no bottle provides, so it cannot start in a FROM-scratch tree."

edits = [
  {
    #
    # Same cause as gnu.org/coreutils: gnulib's m4/selinux-selinux-h.m4
    # declares AC_ARG_WITH([selinux], ..., [], [with_selinux=maybe]) -- `maybe`
    # is AUTO-DETECTION -- and coreutils' configure.ac spells out "Honor
    # --without-selinux to force disable". On a builder that HAS libselinux,
    # which every Ubuntu one does, the bottle comes out linked against a
    # library nothing in this factory provides.
    #
    # Found by `bk builder`'s soname census over the staged s390x rootfs
    # (go-pkgx/bk#276), which named every offender at once instead of one per
    # failed build:
    #   libselinux.so.1 <- gnu.org/findutils/v4.11.0/bin/find
    #                      gnu.org/gettext/v1.0/lib/libgettextlib-1.0.so
    #                      gnu.org/sed/v4.10/bin/sed (+1 more)
    #
    # An unrecognised --without-X is a WARNING in autoconf, not an error, so
    # this is safe on a release whose gnulib import lacks the module. The
    # census after the rebuild is the control, not this comment.
    path = "build.script"
    from = "--disable-debug"
    to   = "--disable-debug --without-selinux"
  },
]
