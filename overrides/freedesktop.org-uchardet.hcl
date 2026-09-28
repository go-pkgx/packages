project = "freedesktop.org/uchardet"
why     = "converted from the unified diff overrides; the reason is in the git history"

edits = [
  {
    path = "build.env.ARGS"
    append = [
      "-DCMAKE_POLICY_VERSION_MINIMUM=3.5",
    ]
  },
]
