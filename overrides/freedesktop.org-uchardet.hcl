project = "freedesktop.org/uchardet"
why     = "uchardet asks for a `cmake_minimum_required` below 3.5, which CMake 4 removed support for. CMAKE_POLICY_VERSION_MINIMUM restores it without touching the source."

edits = [
  {
    path = "build.env.ARGS"
    append = [
      "-DCMAKE_POLICY_VERSION_MINIMUM=3.5",
    ]
  },
]
