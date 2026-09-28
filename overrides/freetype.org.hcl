project = "freetype.org"
why     = "freetype links brotli and did not declare it, so the published darwin bottle asks dyld for a path that exists on the runner that built it and on no user's machine."

edits = [
  {
    path = "build.env.ARGS"
    append = [
      "-DFT_REQUIRE_ZLIB=TRUE",
      "-DFT_REQUIRE_BZIP2=TRUE",
      "-DFT_REQUIRE_PNG=TRUE",
      "-DFT_REQUIRE_BROTLI=TRUE",
    ]
  },
  {
    path = "dependencies[\"github.com/google/brotli\"]"
    set  = "*"
  },
]
