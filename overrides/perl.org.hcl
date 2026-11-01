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
# ⛔ THE BOUND IS A MOVING CEILING, AND UPSTREAM HAS ALREADY MOVED IT ONCE.
# pkgxdev/pantry 22270b11, 2026-03-06:
#
#   fix(perl): relax LLVM version constraint from <17 to <19 on Linux
#
# So it is not a known incompatibility between perl and clang; it is "we have
# not tested newer", raised when something needed it raised. That is the best
# available answer to why no reason was written down: there was no reason to
# write, only a version nobody had tried. It also means `>=18` is not a
# gamble against a documented constraint — it is the same move upstream made,
# one ceiling further.
#
# ⛔ AND THE TEST DOES NOT BELONG ON x86-64, for a reason I weighed second.
# x86-64 has a control — it builds perl today with 18.1.8 — but perl.org IS
# PUBLISHED there, so forcing a rebuild replaces a production bottle under its
# existing tag. That is exactly the tag-replacement go-pkgx/packages#393 is
# open about, and spending a production bottle to answer a question is the
# wrong trade when a free venue exists.
#
# The free venue is the s390x seed registry: `RECIPES="perl.org"
# ./seed/dispatch-gen1.sh` publishes to the throwaway loopback registry on the
# VM and replaces nothing. perl is not published for s390x at all, so there is
# nothing there to overwrite either.
#
# What that costs in attribution, stated so nobody has to rediscover it: a
# failure on s390x is NOT automatically about the compiler. It is readable
# only if the error names the compiler or the long-double ABI; any other
# failure is a fact about the bootstrap, not about this bound, and must not be
# reported as one.
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
