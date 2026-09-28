project = "gnu.org/gcc"
why     = "gcc pins `gnu.org/gcc: 14` as its own linux build dependency, for every version. `14` is a caret range, so it means major 14 and nothing else, and our registry starts at 16.2.0 — let the newest gcc we have build the older one."

edits = [
  {
    path = "build.dependencies.linux[\"gnu.org/gcc\"]"
    set  = ">=14"
  },
  {
    path = "build.env.darwin.ARGS"
    append = [
      "--disable-multilib",
    ]
  },
  {
    path = "build.env.s390x"
    set = {
      "TRIPLET" = "s390x-linux-gnu"
    }
  },
  {
    path = "build.script"
    set = [
      {
        "if" = "darwin"
        "run" = [
          "if test -n \"$PATCH{{version.major}}{{version.minor}}\"; then",
          "curl \"$PATCH{{version.major}}{{version.minor}}\" | patch -p1",
          "fi",
        ]
        "working-directory" = ".."
      },
      {
        "if" = "darwin/aarch64"
        "run" = [
          "if test -n \"$BRANCH{{version.major}}{{version.minor}}\"; then",
          "curl -L \"$BRANCH{{version.major}}{{version.minor}}\" | tar xz --strip-components=1",
          "fi",
        ]
        "working-directory" = ".."
      },
      {
        "if" = "^14.2 || >=15.2"
        "run" = [
          "if test \"{{hw.platform}}/{{hw.arch}}\" = \"darwin/x86-64\"; then",
          "patch -p1 < props/disable-cfi-x86-64-darwin.patch",
          "sed -i 's/ i386\\/t-msabi//g' libgcc/config.host",
          "patch -p1 < props/remove-old-frame-symbols-darwin.patch",
          "fi",
        ]
        "working-directory" = ".."
      },
      "ARGS=($ARGS --with-pkgversion=\"pkgx GCC {{version}}\")",
      {
        "if"  = "linux"
        "run" = "export ARGS=(\"$${ARGS[@]}\" --with-boot-ldflags=\"-static-libstdc++ -static-libgcc $LDFLAGS\")"
      },
      {
        "if"  = "linux"
        "run" = <<EOT
if [ {{version.major}} -ge 6 ];  then ARGS=("$${ARGS[@]}" --enable-default-pie); fi
if [ {{version.major}} -ge 9 ];  then ARGS=("$${ARGS[@]}" --enable-pie-tools); fi
if [ {{version.major}} -ge 13 ]; then ARGS=("$${ARGS[@]}" --enable-host-pie); fi
export ARGS
EOT
      },
      {
        "if"  = "<10"
        "run" = "ARGS=(\"$${ARGS[@]}\" --disable-lto --disable-plugin); export ARGS"
      },
      {
        "if"  = "linux"
        "run" = <<EOT
DEPRP="{{deps.gnu.org/mpc.prefix}}/lib:{{deps.gnu.org/mpfr.prefix}}/lib:{{deps.gnu.org/gmp.prefix}}/lib:{{deps.zlib.net.prefix}}/lib"
export LDFLAGS="$LDFLAGS -Wl,-rpath,$DEPRP -Wl,-rpath-link,$DEPRP"
EOT
      },
      {
        "if"  = "darwin"
        "run" = "export LDFLAGS_FOR_TARGET=\"$LDFLAGS\""
      },
      "../configure \"$${ARGS[@]}\"",
      "make --jobs {{ hw.concurrency }}",
      "make install",
      {
        "if"  = "linux"
        "run" = <<EOT
if [ {{version.major}} -lt 10 ]; then
  for d in {{prefix}}/lib/gcc/*/{{version.raw}}/include-fixed; do
    [ -d "$d/bits" ] || continue
    mv "$d/bits" "$d/bits.disabled-by-pantry" || true
    mkdir -p "$d/bits"
  done
  find {{prefix}}/lib/gcc -name 'libgcc*.a' -o -name 'crt*.o' 2>/dev/null | while read f; do
    {{deps.gnu.org/binutils.prefix}}/bin/strip --strip-debug "$f" 2>/dev/null || true
  done
fi
EOT
      },
      {
        "if"   = "darwin"
        "prop" = <<EOT
/#define .*STDIO/a\
#include <stddef.h>\
#include <_stdio.h>
EOT
        "run" = [
          "for hdr in lib/gcc/*/*/include-fixed/stdio.h; do",
          "if ! grep -q 'extern FILE \\*__stdinp' \"$hdr\" || grep -q '#include <_stdio.h>' \"$hdr\"; then continue; fi",
          "sed -i -f $PROP \"$hdr\"",
          "done",
        ]
        "working-directory" = "$${{prefix}}"
      },
      {
        "run"               = "test -f gc++ || ln -sf c++ gc++"
        "working-directory" = "$${{prefix}}/bin"
      },
      {
        "run" = [
          "ln -sf gcc cc",
          "ln -sf ../../../binutils/v\\*/bin/ar ar",
          "ln -sf ../../../binutils/v\\*/bin/nm nm",
          "ln -sf ../../../binutils/v\\*/bin/ranlib ranlib",
        ]
        "working-directory" = "$${{prefix}}/bin"
      },
      {
        "if" = "darwin/x86-64"
        "run" = [
          "if test -f libgcc_s.1.1.dylib; then",
          "codesign --remove-signature libgcc_s.1.1.dylib || true",
          "codesign -s - --force libgcc_s.1.1.dylib",
          "fi",
          "if test -f libgcc_s.1.dylib; then",
          "codesign --remove-signature libgcc_s.1.dylib || true",
          "codesign -s - --force libgcc_s.1.dylib",
          "fi",
        ]
        "working-directory" = "$${{prefix}}/lib"
      },
      {
        "if" = "linux"
        "run" = [
          "for tool in gcc g++ cpp c++ ar nm ranlib; do",
          "if test -n \"$TRIPLET\" && test -e \"$tool\"; then ln -sf \"$tool\" \"$TRIPLET-$tool\"; fi",
          "done",
        ]
        "working-directory" = "$${{prefix}}/bin"
      },
    ]
    expect = [
      {
        "if" = "darwin"
        "run" = [
          "if test -n \"$PATCH{{version.major}}{{version.minor}}\"; then",
          "curl \"$PATCH{{version.major}}{{version.minor}}\" | patch -p1",
          "fi",
        ]
        "working-directory" = ".."
      },
      {
        "if" = "darwin/aarch64"
        "run" = [
          "if test -n \"$BRANCH{{version.major}}{{version.minor}}\"; then",
          "curl -L \"$BRANCH{{version.major}}{{version.minor}}\" | tar xz --strip-components=1",
          "fi",
        ]
        "working-directory" = ".."
      },
      {
        "if" = "^14.2 || >=15.2"
        "run" = [
          "if test \"{{hw.platform}}/{{hw.arch}}\" = \"darwin/x86-64\"; then",
          "patch -p1 < props/disable-cfi-x86-64-darwin.patch",
          "sed -i 's/ i386\\/t-msabi//g' libgcc/config.host",
          "patch -p1 < props/remove-old-frame-symbols-darwin.patch",
          "fi",
        ]
        "working-directory" = ".."
      },
      "ARGS=($ARGS --with-pkgversion=\"pkgx GCC {{version}}\")",
      {
        "if"  = "linux"
        "run" = "export ARGS=(\"$${ARGS[@]}\" --with-boot-ldflags=\"-static-libstdc++ -static-libgcc $LDFLAGS\")"
      },
      {
        "if"  = "linux"
        "run" = <<EOT
if [ {{version.major}} -ge 6 ];  then ARGS=("$${ARGS[@]}" --enable-default-pie); fi
if [ {{version.major}} -ge 9 ];  then ARGS=("$${ARGS[@]}" --enable-pie-tools); fi
if [ {{version.major}} -ge 13 ]; then ARGS=("$${ARGS[@]}" --enable-host-pie); fi
export ARGS
EOT
      },
      {
        "if"  = "<10"
        "run" = "ARGS=(\"$${ARGS[@]}\" --disable-lto --disable-plugin); export ARGS"
      },
      {
        "if"  = "linux"
        "run" = <<EOT
DEPRP="{{deps.gnu.org/mpc.prefix}}/lib:{{deps.gnu.org/mpfr.prefix}}/lib:{{deps.gnu.org/gmp.prefix}}/lib:{{deps.zlib.net.prefix}}/lib"
export LDFLAGS="$LDFLAGS -Wl,-rpath,$DEPRP -Wl,-rpath-link,$DEPRP"
EOT
      },
      {
        "if"  = "darwin"
        "run" = "export LDFLAGS_FOR_TARGET=\"$LDFLAGS\""
      },
      "../configure \"$${ARGS[@]}\"",
      "make --jobs {{ hw.concurrency }}",
      "make install",
      {
        "if"  = "linux"
        "run" = <<EOT
if [ {{version.major}} -lt 10 ]; then
  for d in {{prefix}}/lib/gcc/*/{{version.raw}}/include-fixed; do
    [ -d "$d/bits" ] || continue
    mv "$d/bits" "$d/bits.disabled-by-pantry" || true
    mkdir -p "$d/bits"
  done
  find {{prefix}}/lib/gcc -name 'libgcc*.a' -o -name 'crt*.o' 2>/dev/null | while read f; do
    {{deps.gnu.org/binutils.prefix}}/bin/strip --strip-debug "$f" 2>/dev/null || true
  done
fi
EOT
      },
      {
        "if"   = "darwin"
        "prop" = <<EOT
/#define .*STDIO/a\
#include <stddef.h>\
#include <_stdio.h>
EOT
        "run" = [
          "for hdr in lib/gcc/*/*/include-fixed/stdio.h; do",
          "if ! grep -q 'extern FILE \\*__stdinp' \"$hdr\" || grep -q '#include <_stdio.h>' \"$hdr\"; then continue; fi",
          "sed -i -f $PROP \"$hdr\"",
          "done",
        ]
        "working-directory" = "$${{prefix}}"
      },
      {
        "run"               = "test -f gc++ || ln -sf c++ gc++"
        "working-directory" = "$${{prefix}}/bin"
      },
      {
        "run" = [
          "ln -sf gcc cc",
          "ln -sf ../../../binutils/v\\*/bin/ar ar",
          "ln -sf ../../../binutils/v\\*/bin/nm nm",
          "ln -sf ../../../binutils/v\\*/bin/ranlib ranlib",
        ]
        "working-directory" = "$${{prefix}}/bin"
      },
      {
        "if" = "darwin/x86-64"
        "run" = [
          "if test -f libgcc_s.1.1.dylib; then",
          "codesign --remove-signature libgcc_s.1.1.dylib || true",
          "codesign -s - --force libgcc_s.1.1.dylib",
          "fi",
          "if test -f libgcc_s.1.dylib; then",
          "codesign --remove-signature libgcc_s.1.dylib || true",
          "codesign -s - --force libgcc_s.1.dylib",
          "fi",
        ]
        "working-directory" = "$${{prefix}}/lib"
      },
      {
        "if" = "linux"
        "run" = [
          "for tool in gcc g++ cpp c++ ar nm ranlib; do",
          "if test -e \"$tool\"; then ln -sf \"$tool\" \"$TRIPLET-$tool\"; fi",
          "done",
        ]
        "working-directory" = "$${{prefix}}/bin"
      },
    ]
  },
]
