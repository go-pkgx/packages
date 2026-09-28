project = "freetype.org"
why     = "converted from freetype.org-brotli.patch; the reason is in the git history of those files"

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
    set = "*"
  },
]
