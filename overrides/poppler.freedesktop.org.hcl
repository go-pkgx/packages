project = "poppler.freedesktop.org"
why     = "converted from poppler.freedesktop.org-tiff-hint.patch; the reason is in the git history of those files"

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
