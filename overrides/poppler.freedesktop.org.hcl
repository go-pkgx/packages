project = "poppler.freedesktop.org"
why     = "poppler's darwin/aarch64 rebuild stops at configure: CMake goes looking for libtiff instead of using the one we declared, and finds the runner's."

edits = [
  {
    path = "build.env.darwin"
    set = {
      "ARGS" = [
        "-DTIFF_INCLUDE_DIR={{deps.simplesystems.org/libtiff.prefix}}/include",
        "-DTIFF_LIBRARY={{deps.simplesystems.org/libtiff.prefix}}/lib/libtiff.dylib",
      ]
    }
  },
]
