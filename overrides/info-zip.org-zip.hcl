project = "info-zip.org/zip"
why     = "Upstream's recipe builds zip 3.0 (2008) against Debian's 3.0-11 patch series with a bare CC, and clang 16+ refuses the K&R C that remains; it also downloads over PLAIN HTTP from SourceForge, and this is the only seed package that does."

edits = [
  {
    path = "build.script"
    set = [
      {
        "run"               = "wget https://deb.debian.org/debian/pool/main/z/zip/zip_3.0-13.debian.tar.xz && tar xf zip_3.0-13.debian.tar.xz"
        "working-directory" = "patch"
      },
      "patch -p1 < patch/debian/patches/01-typo-it-is-transferring-not-transfering.patch",
      "patch -p1 < patch/debian/patches/02-typo-it-is-privileges-not-priviliges.patch",
      "patch -p1 < patch/debian/patches/03-manpages-in-section-1-not-in-section-1l.patch",
      "patch -p1 < patch/debian/patches/04-do-not-set-unwanted-cflags.patch",
      "patch -p1 < patch/debian/patches/05-typo-it-is-preceding-not-preceeding.patch",
      "patch -p1 < patch/debian/patches/06-stack-markings-to-avoid-executable-stack.patch",
      "patch -p1 < patch/debian/patches/07-fclose-in-file-not-fclose-x.patch",
      "patch -p1 < patch/debian/patches/08-hardening-build-fix-1.patch",
      "patch -p1 < patch/debian/patches/09-hardening-build-fix-2.patch",
      "patch -p1 < patch/debian/patches/10-remove-build-date.patch",
      # ${CC:-…} rather than the bottle's gcc outright.
      #
      # bk exports CC with the SOVEREIGN SYSROOT in pkgx-libc mode —
      #
      #   export CC="${CC:-clang --sysroot=… -isystem …}"
      #
      # — and naming an absolute compiler threw all of it away. That is why
      # the second sovereign generation failed here with
      #
      #   unix/osdep.h:30:10: fatal error: sys/types.h: No such file or directory
      #
      # the same shape as go-pkgx/bk#296, where a test sandbox lost the
      # sysroot by naming a compiler instead of using the one bk had set.
      #
      # The fallback keeps the DISTRIBUTION path byte-for-byte what it was:
      # off the sovereign lane nothing exports CC and the bottle's gcc is
      # still what runs. `expect` below is untouched, because what upstream
      # ships has not moved.
      "make -f unix/Makefile CC=\"$${CC:-{{deps.gnu.org/gcc.prefix}}/bin/gcc} -std=gnu17 -Wno-implicit-function-declaration -Wno-implicit-int -Wno-int-conversion\" generic",
      "make -f unix/Makefile BINDIR={{prefix}}/bin MANDIR={{prefix}}/man/man1 install",
    ]
    expect = [
      {
        "run"               = "wget https://deb.debian.org/debian/pool/main/z/zip/zip_3.0-11.debian.tar.xz && tar xf zip_3.0-11.debian.tar.xz"
        "working-directory" = "patch"
      },
      "patch -p1 < patch/debian/patches/01-typo-it-is-transferring-not-transfering",
      "patch -p1 < patch/debian/patches/02-typo-it-is-privileges-not-priviliges",
      "patch -p1 < patch/debian/patches/03-manpages-in-section-1-not-in-section-1l",
      "patch -p1 < patch/debian/patches/04-do-not-set-unwanted-cflags",
      "patch -p1 < patch/debian/patches/05-typo-it-is-preceding-not-preceeding",
      "patch -p1 < patch/debian/patches/06-stack-markings-to-avoid-executable-stack",
      "patch -p1 < patch/debian/patches/07-fclose-in-file-not-fclose-x",
      "patch -p1 < patch/debian/patches/08-hardening-build-fix-1",
      "patch -p1 < patch/debian/patches/09-hardening-build-fix-2",
      "patch -p1 < patch/debian/patches/10-remove-build-date",
      "make -f unix/Makefile CC={{deps.gnu.org/gcc.prefix}}/bin/gcc generic",
      "make -f unix/Makefile BINDIR={{prefix}}/bin MANDIR={{prefix}}/man/man1 install",
    ]
  },
  {
    #
    # http:// -> https://, because this is a SEED package and the seed is the
    # foundation the sovereign generation is built on.
    #
    # Census over the pantry's 1874 distributables: 1716 https, 138 git+https,
    # 19 http, 1 ftp. Of the 23 projects fetched in the clear, exactly ONE is
    # in seed/order.txt, and this is it.
    #
    # It matters more than the count suggests. bk DOES verify a source
    # checksum when the recipe gives one, but 1 recipe of 904 gives one --
    # openssl.org, whose `sha` is a sibling `.sha256` on the SAME host, so it
    # catches corruption and not substitution (go-pkgx/bk#282). For everything
    # else TLS is the only control there is, and here there was none at all.
    # The tarball is then extracted as root inside the chroot, and
    # go-pkgx/bottle#107 is a traversal in that extractor.
    #
    # Measured before changing it, because a URL change that also changes the
    # content is a different defect: the same path over http and over https
    # returns byte-identical content,
    #   f0e8bb1f9b7eb0b01285495a2699df3a4b766784c1765a8f1aeedf63c0806369
    #   1118845 bytes, both
    # so this changes the transport and nothing else.
    #
    # A substitution rather than a `set`: if upstream fixes its own URL, this
    # reports PremiseGone instead of silently pinning a URL nobody maintains.
    #
    path = "distributable.url"
    from = "http://downloads.sourceforge.net/"
    to   = "https://downloads.sourceforge.net/"
  },
]
