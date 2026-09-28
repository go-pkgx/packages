project = "libcxx.llvm.org"
why     = "converted from libcxx.llvm.org-no-atomic-lib.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.env.ARGS"
    append = [
      "-DLIBCXX_HAS_ATOMIC_LIB=OFF",
    ]
  },
]
