project = "openjpeg.org"
why     = "opj_compress links little-cms and nothing declared it. CMake found one on the RUNNER, and bk refused to publish a result that names a library no consumer has."

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
    from = "-DCMAKE_BUILD_TYPE=Release\nmake"
    to   = "-DCMAKE_BUILD_TYPE=Release $LCMS_ARGS\nmake"
  },
  {
    path = "dependencies[\"littlecms.com\"]"
    set  = "^2"
  },
]
