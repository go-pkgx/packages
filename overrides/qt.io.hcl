project = "qt.io"
why     = "converted from qt.io-libcxx-removed-unary-function.patch, qt.io-source-moved-to-archive.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.env.darwin.ARGS"
    append = [
      "QMAKE_CXXFLAGS+=-D_LIBCPP_ENABLE_CXX17_REMOVED_UNARY_BINARY_FUNCTION",
      "QMAKE_CXXFLAGS+=-Wno-error=enum-constexpr-conversion",
    ]
  },
  {
    path = "distributable.url"
    from = "official_releases"
    to   = "archive"
  },
]
