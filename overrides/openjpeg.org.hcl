project = "openjpeg.org"
why     = "converted from openjpeg.org-declares-littlecms.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.env"
    set = {
      "LCMS_ARGS" = ""
      "darwin" = {
        "LCMS_ARGS" = [
          "-DLCMS2_INCLUDE_DIR={{deps.littlecms.com.prefix}}/include",
          "-DLCMS2_LIBRARY={{deps.littlecms.com.prefix}}/lib/liblcms2.dylib",
        ]
      }
    }
  },
  {
    path = "build.script"
    from = "se"
    to   = "se $LCMS_ARGS"
  },
  {
    path = "dependencies[\"littlecms.com\"]"
    set = "^2"
  },
]
