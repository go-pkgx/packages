project = "perl.org"
why     = "an UNEXPLAINED upper bound on the compiler, `llvm.org: <19`, which no architecture can satisfy unless an old llvm happens to be published for it. On linux/s390x the only published llvm is 23.1.2, so perl.org cannot build there at all — and perl blocks four of the nine unresolved toolchain roots (gnu.org/autoconf, gnu.org/automake, gnu.org/help2man, gnu.org/texinfo)."

# ⛔ THIS IS A MEASUREMENT, NOT A FIX, AND IT IS WRITTEN TO BE DISPROVED.
#
# The recipe says, with no comment beside it:
#
#   build.dependencies.linux:
#     llvm.org: <19
#
# A ceiling with no stated reason is not the same thing as a ceiling with a
# reason nobody wrote down, and the two are indistinguishable from here. What
# IS measurable is what the bound costs, and what happens without it.
#
# What it costs (2026-11-01 11:58 CET, `pkgx --graph --platform …`):
#
#   linux/x86-64   llvm.org@<19  ->  18.1.8        perl builds today
#   linux/s390x    llvm.org      ->  23.1.2        nothing satisfies <19
#
# So the bound is not wrong in itself — it is unsatisfiable on an architecture
# that has no old llvm, and s390x is the first one. The same will be true of
# riscv64, ppc64le and loong64 the day they are bootstrapped.
#
# ⛔ AND THE TEST BELONGS ON x86-64, NOT ON s390x. x86-64 builds perl today
# with 18.1.8, so it HAS a control: if the build goes red with 23.1.2 and
# nothing else changed, the bound is still needed and the error names why —
# which is the thing the recipe never recorded. Testing this on s390x first
# would confound the compiler change with every other reason an unbootstrapped
# architecture fails, and a failure there would say nothing about the bound.
#
# `>=18` rather than `*`: 18.1.8 is the version demonstrably known to work, so
# it is the honest floor. A bare `*` would record no knowledge at all, and the
# next person would be where I am now.
#
# If the x86-64 build is green with 23.1.2, this line is the whole fix and it
# unblocks four roots on every architecture. If it is red, the error goes in
# the `why` above and the bound comes back as `<19` WITH its reason, which is
# strictly better than what is there now.
#
# Refs go-pkgx/packages#316.

merge {
  build {
    dependencies {
      # Merged at the leaf on purpose: `build.dependencies.linux` also holds
      # `gnu.org/make`, and replacing the whole table would delete it. An
      # override that replaces a field it meant to amend is a defect this
      # repository has already paid for once.
      linux = {
        "llvm.org" = ">=18"
      }
    }
  }
}
