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
    path = "build.script"
    from = "./configure --prefix=\"{{prefix}}\""
    to   = <<EOT
case "{{hw.target}}" in *s390x*) CFLAGS="$CFLAGS -fzvector" ;; esac
./configure --prefix="{{prefix}}"
EOT
  },
]
