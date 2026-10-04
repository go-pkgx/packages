project = "llvm.org"
why     = "llvm records a darwin rpath that exists only on the runner that built it, links libxml2 without declaring it, and asks for semverator on every platform although only the darwin build path uses it. And on linux/s390x the libxml2 it is held to is the wrong LINE: ld.lld NEEDs libxml2.so.16, which gnome.org/libxml2 2.15 provides and 2.13 does not — 2.13's soname is libxml2.so.2 — so `^2` installs a library whose soname the linker cannot use. The failure is not subtle once seen and invisible before: `ld.lld: error while loading shared libraries: libxml2.so.16`, reported as `cc: error: unable to execute command`, from github.com/westes/flex, which has nothing to do with libxml2. Builds never met it because a build's environment drags libxml2 in through the base toolchain; `bk factory --test-only` gives a package only itself and its test.dependencies, and that lean environment is the first llvm's linker ever ran in. This is go-pkgx/packages#233's class — a soname that moved INSIDE a major, so a caret is not an ABI bound — and it is scoped to linux/s390x because that is where it was measured: a bottle links the soname that was in the store when it was built, and tightening the constraint for an arch whose bottle links .so.2 would break it instead."

edits = [
  {
    path   = "build.dependencies[\"crates.io/semverator\"]"
    remove = true
  },
  {
    path = "build.dependencies.darwin"
    set = {
      "crates.io/semverator" = "*"
    }
  },
  {
    path = "build.env.darwin.ARGS"
    prepend = [
      "-DCMAKE_INSTALL_RPATH=\"@loader_path/../lib;@loader_path/../../..\"",
    ]
  },
  {
    path = "dependencies[\"gnome.org/libxml2\"]"
    set  = "^2"
  },
  {
    # Measured: the platform key wins for s390x and the others keep "^2"
    #   linux/s390x  -> gnome.org/libxml2^2.15
    #   linux/x86-64 -> gnome.org/libxml2^2
    path = "dependencies[\"linux/s390x\"][\"gnome.org/libxml2\"]"
    set  = "^2.15"
  },
  {
    path   = "test.dependencies[\"crates.io/semverator\"]"
    remove = true
  },
  {
    path = "test.dependencies.darwin"
    set = {
      "crates.io/semverator" = "*"
    }
  },
]
