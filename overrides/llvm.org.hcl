project = "llvm.org"
why     = "llvm records a darwin rpath that exists only on the runner that built it, links libxml2 without declaring it, and asks for semverator on every platform although only the darwin build path uses it."

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
