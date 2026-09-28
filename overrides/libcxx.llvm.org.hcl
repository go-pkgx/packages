project = "libcxx.llvm.org"
why     = "libc++ looks for an external libatomic that this toolchain does not ship. LIBCXX_HAS_ATOMIC_LIB=OFF makes it use the compiler's own builtins instead."

edits = [
  {
    path = "build.env.ARGS"
    append = [
      "-DLIBCXX_HAS_ATOMIC_LIB=OFF",
    ]
  },
]
