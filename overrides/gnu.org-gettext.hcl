project = "gnu.org/gettext"
why     = "gettext's configure auto-detects the builder's SELinux and its OpenMP, and links both into libgettextlib and the msg* tools — two libraries no bottle in this factory provides, so they cannot start in a FROM-scratch tree."

edits = [
  {
    # Two host libraries, one cause: configure looks at the MACHINE.
    #
    #   libselinux.so.1 <- gnu.org/gettext/v1.0/lib/libgettextlib-1.0.so
    #   libgomp.so.1    <- gnu.org/gettext/v1.0/bin/msginit
    #                      gnu.org/gettext/v1.0/bin/msgmerge
    #
    # named by `bk builder`'s soname census over the staged s390x rootfs
    # (go-pkgx/bk#276). libgettextlib is the one that matters: msgfmt and its
    # siblings are on the build path of every recipe with a po/ directory.
    #
    # --without-selinux is gnulib's (m4/selinux-selinux-h.m4, default
    # `with_selinux=maybe`, which is auto-detection). --disable-openmp is
    # autoconf's own, from AC_OPENMP. Neither is exotic and an unrecognised
    # --without-X / --disable-X is a WARNING rather than an error, so this is
    # safe on a release that declares only one of them.
    #
    # The census after the rebuild is the control. This comment is not.
    path = "build.script"
    from = "./configure $ARGS"
    to   = "./configure $ARGS --without-selinux --disable-openmp"
  },
]
