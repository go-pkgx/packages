project = "llvm.org"
why     = "converted from llvm.org-darwin-rpath.patch, llvm.org-libxml2.patch, llvm.org-semverator-is-darwin-only.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.dependencies[\"crates.io/semverator\"]"
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
    set = "^2"
  },
  {
    path = "test.dependencies[\"crates.io/semverator\"]"
    remove = true
  },
  {
    path = "test.dependencies.darwin"
    set = {
      "crates.io/semverator" = "*"
    }
  },
]
