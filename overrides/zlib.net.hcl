project = "zlib.net"
why     = "two unrelated faults in one recipe: zlib.net's front page carries only the CURRENT release, so every older version 404s the moment a new one ships — while `versions:` happily lists them all from GitHub. Download where the versions are enumerated. And its s390x CRC32 uses vector intrinsics clang refuses without -fzvector."

edits = [
  {
    path = "distributable.url"
    set  = "https://github.com/madler/zlib/releases/download/v{{version.raw}}/zlib-{{version.raw}}.tar.gz"
  },
  {
    # clang's own header says what is missing:
    #
    #   /pkgx/llvm.org/v23.1.2/lib/clang/23/include/vecintrin.h:12864:2:
    #   error: "Use -fzvector to enable vector extensions"
    #   contrib/crc32vx/crc32_vx.c:79:17: error: call to undeclared function
    #   'vec_insert'
    #
    # GCC spells it -mzvector and enables it with -march; clang keeps the
    # vector extensions behind -fzvector whatever the march. zlib builds
    # contrib/crc32vx unconditionally on s390x, so this is not optional.
    #
    # GUARDED by the TARGET, not appended to the linux CFLAGS list, although
    # `clang -fzvector` is accepted and ignored off s390x — measured on Apple
    # clang 21, which is NOT the llvm.org 23.1.2 this factory compiles with.
    # One measurement on a different compiler is not a licence to change what
    # the other two architectures are given: that is exactly the shape of the
    # mistake that took the seed lane down this morning (go-pkgx/pkgx#60),
    # where a fix for one lane broke a working one.
    #
    # `{{hw.target}}` is the BUILD TARGET's triple, so a cross build is keyed
    # correctly where `uname -m` would answer about the host.
    #
    # -march=z13 BESIDE -fzvector, added 2026-10-05 after the first fix was
    # measured and found incomplete. -fzvector turns the vector language
    # extensions ON; the BUILTINS still need the target feature, which comes
    # from -march. The second sovereign generation said so in as many words,
    # at a different line from the error this override was written for:
    #
    #   vecintrin.h:2625:10: error: '__builtin_s390_vupllf' needs target
    #   feature vector
    #
    # z13 IS NOT A NEW FLOOR HERE, which is the only thing that made this a
    # decision rather than a typo. Go has required z13 as the minimum machine
    # level for s390x since Go 1.19, and every binary this project ships for
    # the architecture -- bk, pkgx, pkgm, all CGO=0 Go -- therefore already
    # refuses to run below it. The runner agrees: `go env` on the LinuxONE
    # machine reports
    #
    #   GOGCCFLAGS='-fPIC -m64 -march=z13 …'
    #
    # z13 is also exactly where the vector facility appears, so the flag the
    # builtins need and the floor the toolchain already imposes are the same
    # number. Raising zlib to it costs nothing that was not already spent.
    path = "build.script"
    from = "./configure --prefix=\"{{prefix}}\""
    to   = <<EOT
case "{{hw.target}}" in *s390x*) CFLAGS="$CFLAGS -fzvector -march=z13" ;; esac
./configure --prefix="{{prefix}}"
EOT
  },
]
