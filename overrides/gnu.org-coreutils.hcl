project = "gnu.org/coreutils"
why     = "coreutils links libintl on darwin and never said so (gettext is in bk's base toolchain, so it is always present while this is BUILT and the closure a consumer installs has a hole in it), and on linux its configure AUTO-DETECTS the host's SELinux and links a library no bottle provides."

edits = [
  {
    path = "dependencies"
    set = {
      "darwin" = {
        "gnu.org/gettext" = "*"
      }
    }
  },
  {
    # --without-selinux, because the default is AUTO-DETECTION and that is how
    # the host gets in. gnulib's m4/selinux-selinux-h.m4 declares
    #   AC_ARG_WITH([selinux], …[do not use SELinux, even on systems with
    #   SELinux]…, [], [with_selinux=maybe])
    # and coreutils' configure.ac says "Honor --without-selinux to force
    # disable". So on a builder that HAS libselinux — every Ubuntu one — the
    # bottle comes out linked against it.
    #
    # Nothing in this factory provides libselinux.so.1. Measured on the s390x
    # sovereign lane, where there is no host to fall back on:
    #   mkdir: error while loading shared libraries: libselinux.so.1:
    #   cannot open shared object file
    # — coreutils is the FIRST thing any build runs, so the whole generation
    # stops there.
    #
    # The trade is deliberate: `ls -Z` and `cp --preserve=context` go away,
    # and a bottle that cannot start anywhere is worth less than one that
    # cannot label a file. Revisit if libselinux is ever packaged; it needs
    # libsepol and pcre2 first.
    #
    # Unconditional rather than keyed to linux: the option comes from gnulib
    # and configure accepts it everywhere, and a platform-keyed script edit
    # would be two places to keep in step for no gain.
    path = "build.script"
    from = "--enable-install-program=kill,uptime"
    to   = "--enable-install-program=kill,uptime --without-selinux"
  },
]
