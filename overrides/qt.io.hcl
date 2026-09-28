project = "qt.io"
why     = "qt 5.15.10 does not build on darwin/aarch64 because libc++ removed std::unary_function, which its headers still use; and its source moved to the upstream archive, so the original URL 404s."

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
    set  = "https://download.qt.io/archive/qt/{{version.marketing}}/{{version}}/single/qt-everywhere-opensource-src-{{version}}.tar.xz"
  },
]
